package main

import (
	"encoding/json"
	"github.com/BurntSushi/toml"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync/atomic"
	"time"
)

type config struct {
	Listen string `toml:"listen"`
	Path   string `toml:"http_path"`
}

const defConf = "config.toml"

var (
	opt = &config{}
	c   atomic.Uint64

	done = make(chan struct{})
)

func main() {
	var conf string
	switch {
	case len(os.Args) < 2:
		conf = defConf
	case len(os.Args) == 2:
		conf = os.Args[1]
	case len(os.Args) > 2:
		log.Fatalf("Usage: %s [config.toml]", os.Args[0])
	}

	if err := parseConfig(conf); err != nil {
		log.Fatal(err)
	}

	httpServer()
}

func parseConfig(configFile string) error {
	if _, err := toml.DecodeFile(configFile, &opt); err != nil {
		return err
	}
	return nil
}

func httpServer() {
	ln, err := net.Listen("tcp", opt.Listen)
	if err != nil {
		log.Fatal(err)
	}
	rtr := mux.NewRouter()
	rtr.HandleFunc(path.Clean(opt.Path), httpClient).Methods(http.MethodGet)
	srv := &http.Server{Handler: rtr}

	log.Printf("Starting HTTP server on \"%s%s\"", opt.Listen, opt.Path)
	log.Fatalf("HTTP server: %v", srv.Serve(ln))
}

func httpClient(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r != nil && r.Body != nil {
			_ = r.Body.Close()
		}
	}()
	v := r.URL.Query()
	if len(v) == 0 {
		errorResp(w, "no params for test provided", http.StatusBadRequest)
		return
	}
	dur := getDuration(v)
	url, err := getUrl(v)
	if err != nil {
		errorResp(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("duration: %vs, url: %s", dur.Seconds(), url)

	cl := &http.Client{Timeout: 300 * time.Millisecond}
	defer cl.CloseIdleConnections()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		errorResp(w, err.Error(), http.StatusInternalServerError)
		return
	}
	c.Swap(0)

	go run(cl, req)
	time.Sleep(dur)
	done <- struct{}{}
	log.Printf("duration: %vs, url: %s, count: %d", dur.Seconds(), url, c.Load())
	if err := jsonResp(w,
		struct{ Count uint64 }{Count: c.Load()},
	); err != nil {
		log.Println(err)
	}
}

func errorResp(w http.ResponseWriter, errMsg string, httpStatus int) {
	http.Error(w, errMsg, httpStatus)
	log.Print(errMsg)
}

func jsonResp(w http.ResponseWriter, v interface{}) error {
	return json.NewEncoder(w).Encode(v)
}

func getDuration(v url.Values) (dur time.Duration) {
	dur = time.Second
	if durStr, ok := v["duration"]; ok && len(durStr) == 1 {
		if d, err := time.ParseDuration(durStr[0]); err == nil {
			dur = d
		}
	}
	return dur
}

func getUrl(v url.Values) (string, error) {
	url, ok := v["url"]
	if !ok || len(url) == 0 {
		return "", errors.New("no \"url\" provided in request")
	}
	return url[0], nil
}

func run(cl *http.Client, req *http.Request) {
	for {
		select {
		case <-done:
			return
		default:
			go request(cl, req)
			//time.Sleep(200 * time.Millisecond) // tmp for test
		}
	}

}

func request(cl *http.Client, req *http.Request) {
	_, _ = cl.Do(req)
	c.Add(1)
}
