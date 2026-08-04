//go:build ladybug && cgo && linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type processObservation struct {
	ExitCode int    `json:"exit_code"`
	Signal   string `json:"signal,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

type killOptions struct {
	MarkerPath  string
	MarkerValue string
	GatePath    string
	ReadDelta   uint64
	Timeout     time.Duration
}

func startAndKill(ctx context.Context, executable string, arguments, extraEnvironment []string, options killOptions) (processObservation, error) {
	command := exec.Command(executable, arguments...)
	command.Env = append(os.Environ(), extraEnvironment...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return processObservation{}, err
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	condition := func() (bool, error) {
		data, err := os.ReadFile(options.MarkerPath)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return strings.TrimSpace(string(data)) == options.MarkerValue, nil
	}
	exited, waitErr, conditionErr := waitForCondition(ctx, finished, options.Timeout, condition)
	if conditionErr != nil {
		_ = command.Process.Kill()
		<-finished
		return observeProcess(command, stdout.String(), stderr.String()), conditionErr
	}
	if exited {
		return observeProcess(command, stdout.String(), stderr.String()), unexpectedExit(waitErr)
	}

	if options.GatePath != "" {
		baselineRead, err := readProcRChar(command.Process.Pid)
		if err != nil {
			_ = command.Process.Kill()
			<-finished
			return observeProcess(command, stdout.String(), stderr.String()), err
		}
		if err := os.WriteFile(options.GatePath, []byte("go\n"), 0o600); err != nil {
			_ = command.Process.Kill()
			<-finished
			return observeProcess(command, stdout.String(), stderr.String()), err
		}
		if options.ReadDelta > 0 {
			condition = func() (bool, error) {
				currentRead, err := readProcRChar(command.Process.Pid)
				if err != nil {
					return false, err
				}
				return currentRead >= baselineRead+options.ReadDelta, nil
			}
			exited, waitErr, conditionErr = waitForCondition(ctx, finished, options.Timeout, condition)
			if conditionErr != nil {
				_ = command.Process.Kill()
				<-finished
				return observeProcess(command, stdout.String(), stderr.String()), conditionErr
			}
			if exited {
				return observeProcess(command, stdout.String(), stderr.String()), unexpectedExit(waitErr)
			}
		}
	}

	if err := command.Process.Kill(); err != nil {
		waitErr := <-finished
		return observeProcess(command, stdout.String(), stderr.String()), errors.Join(err, unexpectedExit(waitErr))
	}
	waitErr = <-finished
	observation := observeProcess(command, stdout.String(), stderr.String())
	if observation.Signal != "SIGKILL" {
		return observation, fmt.Errorf("worker termination signal = %q, want SIGKILL: %v", observation.Signal, waitErr)
	}
	return observation, nil
}

func runChild(ctx context.Context, executable string, arguments, extraEnvironment []string, timeout time.Duration) (processObservation, error) {
	command := exec.Command(executable, arguments...)
	command.Env = append(os.Environ(), extraEnvironment...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return processObservation{}, err
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		_ = command.Process.Kill()
		<-finished
		return observeProcess(command, stdout.String(), stderr.String()), ctx.Err()
	case <-timer.C:
		_ = command.Process.Kill()
		<-finished
		return observeProcess(command, stdout.String(), stderr.String()), fmt.Errorf("worker timed out after %s", timeout)
	case <-finished:
		return observeProcess(command, stdout.String(), stderr.String()), nil
	}
}

func waitForCondition(ctx context.Context, finished <-chan error, timeout time.Duration, condition func() (bool, error)) (bool, error, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		ready, err := condition()
		if err != nil {
			return false, nil, err
		}
		if ready {
			return false, nil, nil
		}
		select {
		case <-ctx.Done():
			return false, nil, ctx.Err()
		case <-timer.C:
			return false, nil, fmt.Errorf("condition not reached after %s", timeout)
		case waitErr := <-finished:
			return true, waitErr, nil
		case <-ticker.C:
		}
	}
}

func readProcRChar(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/io", pid))
	if err != nil {
		return 0, err
	}
	return parseProcRChar(string(data))
}

func parseProcRChar(value string) (uint64, error) {
	for _, line := range strings.Split(value, "\n") {
		field, raw, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(field) != "rchar" {
			continue
		}
		parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse rchar: %w", err)
		}
		return parsed, nil
	}
	return 0, errors.New("rchar not found in proc io")
}

func observeProcess(command *exec.Cmd, stdout, stderr string) processObservation {
	observation := processObservation{ExitCode: -1, Stdout: boundedOutput(stdout), Stderr: boundedOutput(stderr)}
	if command.ProcessState == nil {
		return observation
	}
	observation.ExitCode = command.ProcessState.ExitCode()
	if status, ok := command.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		if status.Signal() == syscall.SIGKILL {
			observation.Signal = "SIGKILL"
		} else {
			observation.Signal = status.Signal().String()
		}
	}
	return observation
}

func boundedOutput(value string) string {
	value = strings.TrimSpace(value)
	const limit = 2_048
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func unexpectedExit(waitErr error) error {
	if waitErr == nil {
		return errors.New("worker exited before the kill point")
	}
	return fmt.Errorf("worker exited before the kill point: %w", waitErr)
}
