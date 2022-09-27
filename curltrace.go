package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.elastic.co/apm/module/apmhttp/v2"
	"go.elastic.co/apm/v2"
)

func flush(tracer *apm.Tracer) {
	ctxFlush, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		tracer.Flush(ctxFlush.Done())
	}()
	for {
		select {
		case <-time.After(50 * time.Millisecond):
			log.Println("Waiting for transaction to be flushed...")
		case <-flushed:
			return
		}
	}
}

type headerFlag http.Header

func (f headerFlag) String() string {
	return ""
}

func (f headerFlag) Set(s string) error {
	i := strings.IndexRune(s, '=')
	if i < 0 {
		return errors.New("missing '='; expected format k=v")
	}
	http.Header(f).Add(s[:i], s[i+1:])
	return nil
}

type client struct {
	httpClient *http.Client
	headers    http.Header
}

func newClient() *client {
	httpClient := http.DefaultClient
	httpClient.Transport = apmhttp.WrapRoundTripper(httpClient.Transport, apmhttp.WithClientTrace())

	return &client{
		httpClient: httpClient,
	}
}

func (c *client) fetch(method, dst string) (string, error) {
	var base string
	if parsed, err := url.Parse(dst); err == nil {
		base = filepath.Join(parsed.Host, parsed.Path)
	} else {
		log.Printf("failed to parse destination url: %s", err)
		base = dst
	}

	tx := apm.DefaultTracer().StartTransaction(fmt.Sprintf("%s %s", method, base), "request")
	ctx := apm.ContextWithTransaction(context.Background(), tx)
	req, _ := http.NewRequestWithContext(ctx, method, dst, nil)
	for k, vs := range c.headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	rsp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer rsp.Body.Close()
	io.Copy(io.Discard, rsp.Body)

	apmhttp.SetTransactionContext(tx, req, &apmhttp.Response{
		StatusCode: rsp.StatusCode,
		Headers:    rsp.Header,
	}, nil)

	traceContext := tx.TraceContext()
	tx.End()
	flush(apm.DefaultTracer())
	traceID := traceContext.Trace.String()
	log.Println(rsp.Proto, rsp.Status, traceID)

	return traceID, nil
}

func (c *client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	traceID, err := c.fetch(http.MethodGet, r.FormValue("dst"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Write([]byte(traceID))
}

func main() {
	headers := headerFlag(http.Header{})
	flag.Var(headers, "H", "key=value header, can be passed more than once")
	addr := flag.String("addr", "localhost:1234", "listen addr")
	daemonize := flag.Bool("d", false, "daemonize and wait for requests")
	method := flag.String("X", http.MethodGet, "HTTP method")
	flag.Parse()

	c := newClient()
	c.headers = http.Header(headers)

	if *daemonize {
		srv := http.Server{
			Addr:    *addr,
			Handler: c,
		}
		log.Printf("listening for requets on http://%s", *addr)
		srv.ListenAndServe()
		return
	}

	if flag.NArg() == 0 {
		fmt.Printf("usage: %s [options] url\n", os.Args[0])
		os.Exit(1)
	}
	dst := flag.Arg(0)
	c.fetch(*method, dst)
}
