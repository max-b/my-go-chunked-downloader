package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	goUrl "net/url"
	"os"
)

var urlFlag = flag.String("url", "", "url to fetch from")
var outFileFlag = flag.String("out", "", "filename with path to download to")
var concurrency = flag.Int("concurrency", 20, "number of goroutines to spawn to download")

func main() {
	flag.Parse()
	fmt.Println(*urlFlag)
	fmt.Println(*outFileFlag)

	url, err := goUrl.ParseRequestURI(*urlFlag)
	if err != nil {
		log.Fatalf("url: %v invalid: %v", *urlFlag, err)
		os.Exit(1)
	}

	if *outFileFlag == "" {
		log.Fatalf("path: %v required", *outFileFlag)
		os.Exit(1)
	}

	// Create the file
	out, err := os.Create(*outFileFlag)
	if err != nil {
		log.Fatalf("error opening file: %v: %v", *outFileFlag, err)
		os.Exit(1)
	}
	defer out.Close()

	err = Download(*url, out)

	if err != nil {
		log.Fatalf("error downloading file: %v: %v", *outFileFlag, err)
		os.Exit(1)
	}
}

type result struct {
	err error
}

type chunkData struct {
	start    int64
	end      int64
	url      url.URL
	writer   io.WriterAt
	finished chan result
}

func min(a int64, b int64) int64 {
	if a < b {
		return a
	}

	return b
}

func Download(url url.URL, outFile io.WriterAt) error {
	head, err := http.Head(url.String())
	if err != nil {
		return err
	}

	defer head.Body.Close()

	acceptRange := head.Header.Get("Accept-Ranges")

	if acceptRange != "bytes" {
		return fmt.Errorf("Accept-Ranges header invalid: %v", acceptRange)
	}

	finished := make(chan result)

	var chunks []chunkData

	for i := int64(0); i < head.ContentLength; {
		end := min(i+(head.ContentLength/int64(*concurrency)),
			head.ContentLength-1)

		chunks = append(chunks, chunkData{
			start:    i,
			end:      end,
			url:      url,
			writer:   outFile,
			finished: finished,
		})
		i = end + 1
	}

	fmt.Println(chunks)

	for _, chunk := range chunks {
		go downloadChunk(chunk)
	}

	err = nil

	for range chunks {
		res := <-finished
		if res.err != nil {
			err = res.err
		}
	}

	return err
}

func downloadChunk(chunk chunkData) {
	var res result

	fmt.Printf("Fetching bytes %d-%d\n", chunk.start, chunk.end)

	defer func() {
		chunk.finished <- res
	}()

	headers := make(http.Header)
	headers.Add("range", fmt.Sprintf("bytes=%d-%d", chunk.start, chunk.end))

	request := http.Request{
		Method: "GET",
		URL:    &chunk.url,
		Header: headers,
	}

	client := &http.Client{}

	resp, err := client.Do(&request)
	if err != nil {
		res.err = fmt.Errorf("chunked request from %d-%d failed: %w", chunk.start, chunk.end, err)
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		res.err = fmt.Errorf("chunked request from %d-%d failed with status: %d %v",
			chunk.start, chunk.end, resp.StatusCode, resp.Status)
	}

	_, err = io.Copy(&chunkWriter{chunk.writer, chunk.start}, resp.Body)
	if err != nil {
		res.err = fmt.Errorf("chunked request from %d-%d failed: %w", chunk.start, chunk.end, err)
	}
}

type chunkWriter struct {
	io.WriterAt
	offset int64
}

func (cw *chunkWriter) Write(b []byte) (int, error) {
	n, err := cw.WriteAt(b, cw.offset)
	cw.offset += int64(n)

	return n, err
}
