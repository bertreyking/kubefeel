package kube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type TerminalMessage struct {
	Type   string `json:"type"`
	Stream string `json:"stream,omitempty"`
	Data   string `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
	Cols   uint16 `json:"cols,omitempty"`
	Rows   uint16 `json:"rows,omitempty"`
}

func StreamPodLogs(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container string,
	tailLines int64,
	writer io.Writer,
	flush func(),
) error {
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("namespace is required")
	}
	if strings.TrimSpace(podName) == "" {
		return fmt.Errorf("pod name is required")
	}

	clientset, err := streamingClientset(runtime)
	if err != nil {
		return err
	}

	options := &corev1.PodLogOptions{
		Follow:     true,
		Timestamps: true,
	}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	if trimmed := strings.TrimSpace(container); trimmed != "" {
		options.Container = trimmed
	}

	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, options).Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()

	buffer := make([]byte, 4096)
	for {
		count, readErr := stream.Read(buffer)
		if count > 0 {
			if _, writeErr := writer.Write(buffer[:count]); writeErr != nil {
				return writeErr
			}
			if flush != nil {
				flush()
			}
		}

		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func UploadPodFile(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container, destinationDir, fileName string,
	content io.Reader,
) error {
	if strings.TrimSpace(fileName) == "" {
		return fmt.Errorf("file name is required")
	}

	targetDir := strings.TrimSpace(destinationDir)
	if targetDir == "" {
		targetDir = "/tmp"
	}
	targetDir = path.Clean(targetDir)
	targetPath := path.Join(targetDir, path.Base(fileName))

	_, stderr, err := streamExecCommand(
		ctx,
		runtime,
		namespace,
		podName,
		container,
		[]string{"sh", "-lc", fmt.Sprintf("mkdir -p %s && cat > %s", shellQuote(targetDir), shellQuote(targetPath))},
		content,
		io.Discard,
		false,
		nil,
	)
	if err != nil {
		if stderr != "" {
			return fmt.Errorf("%s: %w", stderr, err)
		}
		return err
	}

	return nil
}

func DownloadPodFile(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container, remotePath string,
) ([]byte, error) {
	trimmedPath := strings.TrimSpace(remotePath)
	if trimmedPath == "" {
		return nil, fmt.Errorf("path is required")
	}

	var stdout bytes.Buffer
	_, stderr, err := streamExecCommand(
		ctx,
		runtime,
		namespace,
		podName,
		container,
		[]string{"sh", "-lc", fmt.Sprintf("cat %s", shellQuote(path.Clean(trimmedPath)))},
		nil,
		&stdout,
		false,
		nil,
	)
	if err != nil {
		if stderr != "" {
			return nil, fmt.Errorf("%s: %w", stderr, err)
		}
		return nil, err
	}

	return stdout.Bytes(), nil
}

func StreamPodTerminal(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container string,
	messenger TerminalMessenger,
) error {
	command := []string{
		"sh",
		"-c",
		"if [ -x /bin/bash ]; then exec /bin/bash -il; elif [ -x /bin/sh ]; then exec /bin/sh -il; else exec sh; fi",
	}

	stdinReader, stdinWriter := io.Pipe()
	defer stdinReader.Close()

	sizeQueue := newTerminalSizeQueue()
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer stdinWriter.Close()
		for {
			message, err := messenger.Read()
			if err != nil {
				return
			}

			switch message.Type {
			case "input":
				if message.Data != "" {
					if _, writeErr := io.WriteString(stdinWriter, message.Data); writeErr != nil {
						return
					}
				}
			case "resize":
				sizeQueue.Push(message.Cols, message.Rows)
			}
		}
	}()

	stdoutWriter := &terminalOutputWriter{messenger: messenger, stream: "stdout"}
	stderrWriter := &terminalOutputWriter{messenger: messenger, stream: "stderr"}
	_, stderr, err := streamExecCommand(
		ctx,
		runtime,
		namespace,
		podName,
		container,
		command,
		stdinReader,
		stdoutWriter,
		true,
		sizeQueue,
		stderrWriter,
	)

	select {
	case <-done:
	default:
		_ = stdinWriter.Close()
	}

	if err != nil {
		if stderr != "" {
			return fmt.Errorf("%s: %w", stderr, err)
		}
		return err
	}

	return nil
}

type TerminalMessenger interface {
	Read() (TerminalMessage, error)
	Write(TerminalMessage) error
}

type terminalOutputWriter struct {
	messenger TerminalMessenger
	stream    string
}

func (w *terminalOutputWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := w.messenger.Write(TerminalMessage{
		Type:   "output",
		Stream: w.stream,
		Data:   string(p),
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

type terminalSizeQueue struct {
	channel chan remotecommand.TerminalSize
	once    sync.Once
}

func newTerminalSizeQueue() *terminalSizeQueue {
	return &terminalSizeQueue{
		channel: make(chan remotecommand.TerminalSize, 8),
	}
}

func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.channel
	if !ok {
		return nil
	}
	return &size
}

func (q *terminalSizeQueue) Push(cols, rows uint16) {
	if cols == 0 || rows == 0 {
		return
	}
	select {
	case q.channel <- remotecommand.TerminalSize{Width: cols, Height: rows}:
	default:
		select {
		case <-q.channel:
		default:
		}
		q.channel <- remotecommand.TerminalSize{Width: cols, Height: rows}
	}
}

func (q *terminalSizeQueue) Close() {
	q.once.Do(func() {
		close(q.channel)
	})
}

func streamExecCommand(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container string,
	command []string,
	stdin io.Reader,
	stdout io.Writer,
	tty bool,
	sizeQueue remotecommand.TerminalSizeQueue,
	extraOutputs ...io.Writer,
) (string, string, error) {
	if runtime == nil || runtime.Config == nil {
		return "", "", fmt.Errorf("kubernetes config is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return "", "", fmt.Errorf("namespace is required")
	}
	if strings.TrimSpace(podName) == "" {
		return "", "", fmt.Errorf("pod name is required")
	}

	config := rest.CopyConfig(runtime.Config)
	config.Timeout = 0

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", "", err
	}

	request := clientset.CoreV1().
		RESTClient().
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("exec")

	request.VersionedParams(&corev1.PodExecOptions{
		Container: strings.TrimSpace(container),
		Command:   command,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    true,
		TTY:       tty,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(config, "POST", request.URL())
	if err != nil {
		return "", "", err
	}

	var stderrBuffer bytes.Buffer
	stderrWriter := io.Writer(&stderrBuffer)
	if len(extraOutputs) > 0 {
		writers := make([]io.Writer, 0, len(extraOutputs)+1)
		writers = append(writers, &stderrBuffer)
		for _, writer := range extraOutputs {
			if writer != nil {
				writers = append(writers, writer)
			}
		}
		stderrWriter = io.MultiWriter(writers...)
	}

	var stdoutBuffer bytes.Buffer
	stdoutWriter := stdout
	if stdoutWriter == nil {
		stdoutWriter = &stdoutBuffer
	}

	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdoutWriter,
		Stderr:            stderrWriter,
		Tty:               tty,
		TerminalSizeQueue: sizeQueue,
	})

	return stdoutBuffer.String(), stderrBuffer.String(), err
}

func streamingClientset(runtime *Runtime) (kubernetes.Interface, error) {
	if runtime == nil || runtime.Config == nil {
		return nil, fmt.Errorf("kubernetes config is not ready")
	}

	config := rest.CopyConfig(runtime.Config)
	config.Timeout = 0
	return kubernetes.NewForConfig(config)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
