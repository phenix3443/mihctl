package mihomo

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func fetchBytes(rawURL, userAgent, origin string, connectTimeout, maxTime time.Duration) ([]byte, error) {
	client := &http.Client{
		Timeout: maxTime,
		Transport: &http.Transport{
			ResponseHeaderTimeout: connectTimeout,
		},
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if origin != "" {
		req.Header.Set("Referer", origin+"/")
		req.Header.Set("Origin", origin)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func fetchToFile(rawURL, dest, userAgent, origin string, connectTimeout, maxTime time.Duration) error {
	data, err := fetchBytes(rawURL, userAgent, origin, connectTimeout, maxTime)
	if err != nil {
		return err
	}
	file, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}
