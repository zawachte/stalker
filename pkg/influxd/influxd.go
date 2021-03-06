package influxd

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func RunInfluxD(abort <-chan error) error {
	// spin up a new Envoy process
	dirname, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	err = os.RemoveAll(filepath.Join(dirname, ".influxdbv2"))
	if err != nil {
		return err
	}

	/* #nosec */
	cmd := exec.Command("/usr/local/bin/influxd")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-abort:
		if errKill := cmd.Process.Kill(); errKill != nil {
		}

		return err
	case err := <-done:
		return err
	}
}

func WaitForInfluxDReady() {
	for {
		resp, err := http.Get("http://localhost:8086/health")
		if err != nil {
			continue
		}
		if resp.StatusCode == 200 {
			break
		}

		time.Sleep(time.Second * 10)
	}
}
