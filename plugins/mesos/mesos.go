package mesos

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/influxdb/telegraf/plugins"
)

type Mesos struct {
	Urls []string
}

var sampleConfig = `
  # An array of addresses to gather stats about. That is, Mesos masters
  # or/and agents URIs.
  urls = ["http://localhost:5050", "http://localhost:5051"]
`

func (n *Mesos) SampleConfig() string {
	return sampleConfig
}

func (n *Mesos) Description() string {
	return "Read Mesos agents state information"
}

func (n *Mesos) Gather(acc plugins.Accumulator) error {
	var wg sync.WaitGroup

	var (
		done = make(chan struct{}, 1)
		errc = make(chan error, 1)
	)

	for _, u := range n.Urls {
		addr, err := url.Parse(u)
		if err != nil {
			return fmt.Errorf("Unable to parse address '%s': %v", u, err)
		}

		wg.Add(1)
		go func(addr *url.URL) {
			defer wg.Done()
			err := n.gatherUrl(addr, acc)
			if err != nil {
				errc <- err
			}
		}(addr)
	}

	go func() {
		wg.Wait()
		done <- struct{}{}
	}()

	select {
	case <-done:
		return nil
	case err := <-errc:
		return err
	}
}

var tr = &http.Transport{
	ResponseHeaderTimeout: time.Duration(3 * time.Second),
}

var client = &http.Client{Transport: tr}

func (n *Mesos) gatherUrl(addr *url.URL, acc plugins.Accumulator) error {
	resp, err := client.Get(fmt.Sprintf("%s/metrics/snapshot", addr.String()))
	if err != nil {
		return fmt.Errorf("Unabale to make HTTP request %s: %v", addr.String(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected HTTP status %s: %v", addr.String(), resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	metrics := map[string]interface{}{}
	if err := json.Unmarshal(body, &metrics); err != nil {
		return err
	}

	tags := getTags(addr)

	for k, v := range metrics {
		name := strings.Replace(k, "/", "_", -1)
		acc.Add(name, v.(float64), tags)
	}

	return nil
}

func getTags(addr *url.URL) map[string]string {
	h := addr.Host
	_, port, err := net.SplitHostPort(h)
	if err != nil {
		if addr.Scheme == "http" {
			port = "80"
		} else if addr.Scheme == "https" {
			port = "443"
		} else {
			port = ""
		}
	}

	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}

	return map[string]string{"host": host, "port": port}
}

func init() {
	plugins.Add("mesos", func() plugins.Plugin {
		return &Mesos{}
	})
}
