package service

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
)

type DockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
}

type DockerService struct {
	socketPath string
	client     *http.Client
}

func NewDockerService() *DockerService {
	socketPath := "/var/run/docker.sock"
	if envPath := os.Getenv("DOCKER_SOCKET_PATH"); envPath != "" {
		socketPath = envPath
	}

	return &DockerService{
		socketPath: socketPath,
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		},
	}
}

func (s *DockerService) IsAvailable() bool {
	_, err := os.Stat(s.socketPath)
	return err == nil
}

func (s *DockerService) ListContainers(ctx context.Context) ([]DockerContainer, error) {
	if !s.IsAvailable() {
		return nil, errors.New("docker socket not available")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost/containers/json?all=true", nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var containers []DockerContainer
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, err
	}

	// Clean up names (remove leading slashes)
	for i := range containers {
		for j := range containers[i].Names {
			containers[i].Names[j] = strings.TrimPrefix(containers[i].Names[j], "/")
		}
	}

	return containers, nil
}

func (s *DockerService) StreamLogs(ctx context.Context, containerID string, out chan<- string) error {
	if !s.IsAvailable() {
		return errors.New("docker socket not available")
	}

	url := fmt.Sprintf("http://localhost/containers/%s/logs?stdout=1&stderr=1&follow=1&tail=200", containerID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	header := make([]byte, 8)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			_, err := io.ReadFull(resp.Body, header)
			if err != nil {
				return err
			}

			length := binary.BigEndian.Uint32(header[4:8])

			payload := make([]byte, length)
			_, err = io.ReadFull(resp.Body, payload)
			if err != nil {
				return err
			}

			line := strings.TrimRight(string(payload), "\r\n")
			lines := strings.Split(line, "\n")
			for _, l := range lines {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case out <- l:
				}
			}
		}
	}
}
