package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"go.elastic.co/apm"
	"go.elastic.co/apm/module/apmhttp"
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

func main() {
	kibana := flag.String("K", "http://localhost:5601", "kibana base path")
	method := flag.String("X", http.MethodGet, "HTTP method")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Printf("usage: %s [options] url\n", os.Args[0])
		os.Exit(1)
	}
	dst := flag.Arg(0)
	output := os.Stdout

	client := http.DefaultClient
	client.Transport = apmhttp.WrapRoundTripper(client.Transport, apmhttp.WithClientTrace())

	var base string
	if parsed, err := url.Parse(dst); err == nil {
		base = filepath.Join(parsed.Host, parsed.Path)
	} else {
		log.Printf("failed to parse destination url: %s", err)
		base = dst
	}

	tx := apm.DefaultTracer.StartTransaction(fmt.Sprintf("%s %s", *method, base), "request")
	ctx := apm.ContextWithTransaction(context.Background(), tx)
	req, _ := http.NewRequestWithContext(ctx, *method, dst, nil)
	rsp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer rsp.Body.Close()
	fmt.Println(rsp.Proto, rsp.Status)
	io.Copy(output, rsp.Body)
	fmt.Println()

	apmhttp.SetTransactionContext(tx, req, &apmhttp.Response{
		StatusCode: rsp.StatusCode,
		Headers:    rsp.Header,
	}, nil)

	traceContext := tx.TraceContext()
	tx.End()
	flush(apm.DefaultTracer)
	log.Printf("%s/app/apm#/link-to/trace/%s\n", *kibana, traceContext.Trace.String())
}
