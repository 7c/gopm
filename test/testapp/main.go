package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// --- Lifecycle behavior ---
	exitAfter := flag.Duration("exit-after", 0, "Exit cleanly after duration (0=never)")
	crashAfter := flag.Duration("crash-after", 0, "Exit with code 1 after duration (0=never)")
	exitCode := flag.Int("exit-code", 1, "Exit code when crashing")
	crashRandom := flag.Duration("crash-random", 0, "Crash at random interval up to this duration")
	runForever := flag.Bool("run-forever", false, "Run until killed (default if no exit/crash flags)")
	panicAfter := flag.Duration("panic-after", 0, "Trigger panic after duration")

	// --- Output behavior ---
	stdoutEvery := flag.Duration("stdout-every", 0, "Print to stdout at this interval")
	stderrEvery := flag.Duration("stderr-every", 0, "Print to stderr at this interval")
	stdoutMsg := flag.String("stdout-msg", "stdout heartbeat", "Message to print to stdout")
	stderrMsg := flag.String("stderr-msg", "stderr heartbeat", "Message to print to stderr")
	stdoutFlood := flag.Bool("stdout-flood", false, "Flood stdout as fast as possible")
	stdoutSize := flag.Int("stdout-size", 80, "Line length for flood mode (bytes)")

	// --- Resource behavior ---
	allocMB := flag.Int("alloc-mb", 0, "Allocate this many MB of memory and hold it")
	cpuBurn := flag.Int("cpu-burn", 0, "Number of goroutines burning CPU")

	// --- Signal behavior ---
	trapSigterm := flag.Bool("trap-sigterm", false, "Catch SIGTERM and ignore it (test kill escalation)")
	slowShutdown := flag.Duration("slow-shutdown", 0, "On SIGTERM, wait this long before exiting")

	// --- Startup behavior ---
	startDelay := flag.Duration("start-delay", 0, "Sleep this long before doing anything")
	printPID := flag.Bool("print-pid", false, "Print PID to stdout on startup")
	printEnv := flag.String("print-env", "", "Print this env var's value to stdout on startup")

	flag.Parse()

	// --- Startup ---
	if *startDelay > 0 {
		time.Sleep(*startDelay)
	}
	if *printPID {
		fmt.Fprintf(os.Stdout, "PID=%d\n", os.Getpid())
	}
	if *printEnv != "" {
		fmt.Fprintf(os.Stdout, "%s=%s\n", *printEnv, os.Getenv(*printEnv))
	}

	// --- Memory allocation ---
	var memhold []byte
	if *allocMB > 0 {
		memhold = make([]byte, *allocMB*1024*1024)
		for i := range memhold {
			memhold[i] = byte(i)
		}
		_ = memhold
	}

	// --- CPU burn ---
	for i := 0; i < *cpuBurn; i++ {
		go func() {
			for {
				_ = rand.Float64()
			}
		}()
	}

	// --- Signal handling ---
	sigCh := make(chan os.Signal, 1)
	if *trapSigterm {
		signal.Notify(sigCh, syscall.SIGTERM)
		go func() {
			for range sigCh {
				fmt.Fprintln(os.Stderr, "SIGTERM received, ignoring")
			}
		}()
	} else if *slowShutdown > 0 {
		signal.Notify(sigCh, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Fprintf(os.Stderr, "SIGTERM received, shutting down in %s\n", *slowShutdown)
			time.Sleep(*slowShutdown)
			os.Exit(0)
		}()
	}

	// --- Output loops ---
	if *stdoutEvery > 0 {
		go func() {
			tick := time.NewTicker(*stdoutEvery)
			for range tick.C {
				fmt.Fprintln(os.Stdout, *stdoutMsg)
			}
		}()
	}
	if *stderrEvery > 0 {
		go func() {
			tick := time.NewTicker(*stderrEvery)
			for range tick.C {
				fmt.Fprintln(os.Stderr, *stderrMsg)
			}
		}()
	}
	if *stdoutFlood {
		go func() {
			line := make([]byte, *stdoutSize)
			for i := range line {
				line[i] = 'X'
			}
			line[len(line)-1] = '\n'
			for {
				os.Stdout.Write(line)
			}
		}()
	}

	// --- Exit/crash timers ---
	if *panicAfter > 0 {
		go func() {
			time.Sleep(*panicAfter)
			panic("intentional panic")
		}()
	}
	if *crashAfter > 0 {
		go func() {
			time.Sleep(*crashAfter)
			fmt.Fprintf(os.Stderr, "crashing with exit code %d\n", *exitCode)
			os.Exit(*exitCode)
		}()
	}
	if *crashRandom > 0 {
		go func() {
			d := time.Duration(rand.Int63n(int64(*crashRandom)))
			time.Sleep(d)
			fmt.Fprintf(os.Stderr, "random crash after %s\n", d)
			os.Exit(*exitCode)
		}()
	}
	if *exitAfter > 0 {
		go func() {
			time.Sleep(*exitAfter)
			fmt.Fprintln(os.Stdout, "clean exit")
			os.Exit(0)
		}()
	}

	// --- Default: run forever (block on signal) ---
	if *runForever || (*exitAfter == 0 && *crashAfter == 0 && *crashRandom == 0 && *panicAfter == 0) {
		waitCh := make(chan os.Signal, 1)
		signal.Notify(waitCh, syscall.SIGINT, syscall.SIGTERM)
		<-waitCh
		os.Exit(0)
	}

	// Wait for exit triggers
	waitCh := make(chan os.Signal, 1)
	signal.Notify(waitCh, syscall.SIGINT, syscall.SIGTERM)
	<-waitCh
}
