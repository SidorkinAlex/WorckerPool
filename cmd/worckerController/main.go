package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const (
	pipePath          = "/tmp/command_pipe" // Путь к именованному каналу
	maxTotalWorkers   = 50                  // Максимальное общее количество воркеров
	highPriorityMin   = 40                  // Минимальное количество воркеров для очереди высокого приоритета
	mediumPriorityMin = 7                   // Минимальное количество воркеров для очереди среднего приоритета
	lowPriorityMin    = 3                   // Минимальное количество воркеров для очереди низкого приоритета
	mediumPriorityMax = 10                  // Максимальное количество воркеров для очереди среднего приоритета
	lowPriorityMax    = 5                   // Максимальное количество воркеров для очереди низкого приоритета
)

var (
	startFlag   = flag.Bool("start", false, "Start the program")
	commandFlag = flag.String("command", "", "Command to execute")
	queueFlag   = flag.String("queue", "high", "Queue type (high, medium, low)")
)

type CommandQueue struct {
	queue []string
	mu    sync.Mutex
}

func (cq *CommandQueue) Add(command string) {
	cq.mu.Lock()
	defer cq.mu.Unlock()
	cq.queue = append(cq.queue, command)
}

func (cq *CommandQueue) Get() (string, bool) {
	cq.mu.Lock()
	defer cq.mu.Unlock()
	if len(cq.queue) == 0 {
		return "", false
	}
	cmd := cq.queue[0]
	cq.queue = cq.queue[1:]
	return cmd, true
}

func worker(id int, cq *CommandQueue, wg *sync.WaitGroup, sem chan struct{}) {
	defer wg.Done()
	for {
		command, ok := cq.Get()
		if !ok {
			time.Sleep(100 * time.Millisecond) // Если очередь пуста, ждем 0.1 секунды
			continue
		}

		// Занимаем слот в семафоре
		sem <- struct{}{}

		fmt.Printf("Worker %d executing: %s\n", id, command)
		cmd := exec.Command("bash", "-c", command)
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Worker %d error: %s\n", id, err)
		} else {
			fmt.Printf("Worker %d output: %s\n", id, string(output))
		}

		// Освобождаем слот в семафоре
		<-sem
	}
}

func startPipeListener(queues map[string]*CommandQueue) {
	// Удаляем старый канал, если он существует
	if _, err := os.Stat(pipePath); err == nil {
		os.Remove(pipePath)
	}

	// Создаем именованный канал
	err := syscall.Mkfifo(pipePath, 0666)
	if err != nil {
		fmt.Printf("Error creating named pipe: %s\n", err)
		return
	}
	defer os.Remove(pipePath)

	fmt.Printf("Listening for commands on pipe: %s\n", pipePath)

	for {
		// Открываем канал для чтения
		pipe, err := os.OpenFile(pipePath, os.O_RDONLY, os.ModeNamedPipe)
		if err != nil {
			fmt.Printf("Error opening pipe: %s\n", err)
			continue
		}

		// Читаем команды из канала
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" {
				// Разделяем команду и тип очереди
				var command, queueType string
				fmt.Sscanf(line, "%s %s", &command, &queueType)
				if queue, exists := queues[queueType]; exists {
					queue.Add(command)
					fmt.Printf("Command added to %s queue: %s\n", queueType, command)
				} else {
					fmt.Printf("Unknown queue type: %s\n", queueType)
				}
			}
		}

		pipe.Close()
	}
}

func manageWorkers(queues map[string]*CommandQueue, wg *sync.WaitGroup, sem chan struct{}) {
	// Управление воркерами для каждой очереди
	highWorkers := highPriorityMin
	mediumWorkers := mediumPriorityMin
	lowWorkers := lowPriorityMin

	for {
		// Проверяем, есть ли задачи в очередях
		if len(queues["high"].queue) > 0 && highWorkers < maxTotalWorkers {
			highWorkers++
			wg.Add(1)
			go worker(highWorkers, queues["high"], wg, sem)
		} else if len(queues["medium"].queue) > 0 && mediumWorkers < mediumPriorityMax {
			mediumWorkers++
			wg.Add(1)
			go worker(mediumWorkers, queues["medium"], wg, sem)
		} else if len(queues["low"].queue) > 0 && lowWorkers < lowPriorityMax {
			lowWorkers++
			wg.Add(1)
			go worker(lowWorkers, queues["low"], wg, sem)
		}

		// Пауза перед следующей проверкой
		time.Sleep(500 * time.Millisecond)
	}
}

func main() {
	flag.Parse()

	if *startFlag {
		fmt.Println("Program started...")

		// Создаем очереди
		queues := map[string]*CommandQueue{
			"high":   &CommandQueue{},
			"medium": &CommandQueue{},
			"low":    &CommandQueue{},
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, maxTotalWorkers)

		// Запуск минимального количества воркеров для каждой очереди
		for i := 0; i < highPriorityMin; i++ {
			wg.Add(1)
			go worker(i, queues["high"], &wg, sem)
		}
		for i := 0; i < mediumPriorityMin; i++ {
			wg.Add(1)
			go worker(i+highPriorityMin, queues["medium"], &wg, sem)
		}
		for i := 0; i < lowPriorityMin; i++ {
			wg.Add(1)
			go worker(i+highPriorityMin+mediumPriorityMin, queues["low"], &wg, sem)
		}

		// Запуск слушателя именованного канала
		go startPipeListener(queues)

		// Управление воркерами
		go manageWorkers(queues, &wg, sem)

		// Ожидание завершения всех воркеров
		wg.Wait()
	} else if *commandFlag != "" && *queueFlag != "" {
		// Отправка команды в именованный канал
		pipe, err := os.OpenFile(pipePath, os.O_WRONLY|os.O_APPEND, os.ModeNamedPipe)
		if err != nil {
			fmt.Printf("Error opening pipe: %s\n", err)
			return
		}
		defer pipe.Close()

		_, err = pipe.WriteString(fmt.Sprintf("%s %s\n", *commandFlag, *queueFlag))
		if err != nil {
			fmt.Printf("Error writing to pipe: %s\n", err)
			return
		}

		fmt.Printf("Command sent to %s queue: %s\n", *queueFlag, *commandFlag)
	} else {
		fmt.Println("Use -start flag to start the program or -command and -queue to add a command.")
	}
}
