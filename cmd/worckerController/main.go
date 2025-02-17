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
	pipePath              = "/tmp/command_pipe" // Путь к именованному каналу
	maxConcurrentCommands = 30                  // Максимальное количество одновременно выполняемых команд
)

var (
	startFlag   = flag.Bool("start", false, "Start the program")
	commandFlag = flag.String("command", "", "Command to execute")
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

func startPipeListener(cq *CommandQueue) {
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
			command := scanner.Text()
			if command != "" {
				cq.Add(command)
				fmt.Printf("Command added to queue: %s\n", command)
			}
		}

		pipe.Close()
	}
}

func main() {
	flag.Parse()

	if *startFlag {
		fmt.Println("Program started...")
		commandQueue := &CommandQueue{}
		var wg sync.WaitGroup
		sem := make(chan struct{}, maxConcurrentCommands)

		// Запуск воркеров
		for i := 0; i < maxConcurrentCommands; i++ {
			wg.Add(1)
			go worker(i, commandQueue, &wg, sem)
		}

		// Запуск слушателя именованного канала
		go startPipeListener(commandQueue)

		// Ожидание завершения всех воркеров
		wg.Wait()
	} else if *commandFlag != "" {
		// Отправка команды в именованный канал
		pipe, err := os.OpenFile(pipePath, os.O_WRONLY|os.O_APPEND, os.ModeNamedPipe)
		if err != nil {
			fmt.Printf("Error opening pipe: %s\n", err)
			return
		}
		defer pipe.Close()

		_, err = pipe.WriteString(*commandFlag + "\n")
		if err != nil {
			fmt.Printf("Error writing to pipe: %s\n", err)
			return
		}

		fmt.Printf("Command sent to queue: %s\n", *commandFlag)
	} else {
		fmt.Println("Use -start flag to start the program or -command to add a command.")
	}
}
