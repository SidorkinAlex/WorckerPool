package main

import (
	"container/list"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const maxConcurrentCommands = 30

type CommandQueue struct {
	queue *list.List
	mu    sync.Mutex
}

func NewCommandQueue() *CommandQueue {
	return &CommandQueue{
		queue: list.New(),
	}
}

func (cq *CommandQueue) Add(command string) {
	cq.mu.Lock()
	defer cq.mu.Unlock()
	cq.queue.PushBack(command)
}

func (cq *CommandQueue) Get() string {
	cq.mu.Lock()
	defer cq.mu.Unlock()
	if cq.queue.Len() == 0 {
		return ""
	}
	element := cq.queue.Front()
	cq.queue.Remove(element)
	return element.Value.(string)
}

func (cq *CommandQueue) IsEmpty() bool {
	cq.mu.Lock()
	defer cq.mu.Unlock()
	return cq.queue.Len() == 0
}

func worker(queue *CommandQueue, wg *sync.WaitGroup, stopChan <-chan struct{}) {
	defer wg.Done()
	for {
		select {
		case <-stopChan:
			log.Println("Worker stopping...")
			return
		default:
			command := queue.Get()
			if command == "" {
				time.Sleep(100 * time.Millisecond) // Ждем, если очередь пуста
				continue
			}

			log.Printf("Executing command: %s", command)
			cmd := exec.Command("bash", "-c", command)
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("Error executing command: %s, Error: %v", command, err)
			} else {
				log.Printf("Command output: %s", string(output))
			}
		}
	}
}

func main() {
	// Обработка флагов
	stopFlag := flag.Bool("stop", false, "Stop the daemon")
	commandFlag := flag.String("command", "", "Command to execute")
	flag.Parse()

	// PID-файл для управления демоном
	pidFile := "/tmp/go_daemon.pid"

	if *stopFlag {
		// Остановка демона
		data, err := os.ReadFile(pidFile)
		if err != nil {
			log.Fatalf("Failed to read PID file: %v", err)
		}
		var pid int
		fmt.Sscanf(string(data), "%d", &pid)
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			log.Fatalf("Failed to stop daemon: %v", err)
		}
		log.Println("Daemon stopped")
		os.Remove(pidFile)
		return
	}

	if *commandFlag != "" {
		// Добавление команды в очередь через файл PID
		data, err := os.ReadFile(pidFile)
		if err != nil {
			log.Fatalf("Failed to read PID file: %v", err)
		}
		var pid int
		fmt.Sscanf(string(data), "%d", &pid)
		if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
			log.Fatalf("Failed to send command to daemon: %v", err)
		}
		log.Printf("Command sent to daemon: %s", *commandFlag)
		return
	}

	log.Println("Daemon started")

	// Создаем очередь команд
	queue := NewCommandQueue()

	// Канал для остановки воркеров
	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	// Запускаем воркеры
	for i := 0; i < maxConcurrentCommands; i++ {
		wg.Add(1)
		go worker(queue, &wg, stopChan)
	}

	// Обработка сигналов для добавления команд
	go func() {
		for {
			var command string
			log.Println("Waiting for command input...")
			_, err := fmt.Scanln(&command)
			if err != nil {
				log.Printf("Error reading input: %v", err)
				continue
			}

			log.Printf("Adding command to queue: %s", command)
			queue.Add(command)
		}
	}()

	// Основной процесс
	for {
		if queue.IsEmpty() {
			time.Sleep(1000 * time.Millisecond) // Спим, если очередь пуста
		}
	}
}
