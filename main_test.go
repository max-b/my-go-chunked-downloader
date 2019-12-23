package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

type FakeWriterAt struct {
	d []byte
}

// NewFakeWriterAt creates and returns a new FakeWriterAt with the given initial size
func NewFakeWriterAt(size int) *FakeWriterAt {
	return &FakeWriterAt{make([]byte, size)}
}

func (wb *FakeWriterAt) WriteAt(dat []byte, off int64) (int, error) {
	// Range/sanity checks.
	if int(off) < 0 {
		return 0, errors.New("offset out of range (too small)")
	}

	// Check fast path extension
	if int(off) == len(wb.d) {
		wb.d = append(wb.d, dat...)
		return len(dat), nil
	}

	// Check slower path extension
	if int(off)+len(dat) >= len(wb.d) {
		nd := make([]byte, int(off)+len(dat))
		copy(nd, wb.d)
		wb.d = nd
	}

	if int(off) > len(wb.d) {
		log.Info(off, int(off))
		return len(dat), nil
	}
	// Once no extension is needed just copy bytes into place.
	copy(wb.d[int(off):], dat)
	return len(dat), nil
}

// Bytes returns the WriteBuffer's underlying data. This value will remain valid so long
// as no other methods are called on the WriteBuffer.
func (wb *FakeWriterAt) Bytes() []byte {
	return wb.d
}

type FakeReader struct {
	d      []byte
	offset int
	length int
}

func (fr *FakeReader) Read(b []byte) (n int, err error) {
	return copy(b, fr.d[fr.offset:fr.offset+fr.length]), nil
	// return fr.length, nil
}

func TestDownload(t *testing.T) {
	fileSize := 1028

	fileToDownload := make([]byte, fileSize)
	_, err := rand.Read(fileToDownload)
	if err != nil {
		t.Fatal("error:", err)
	}

	log.WithFields(log.Fields{
		"file": fileToDownload,
	}).Info("file to download")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Add("Accept-Ranges", "bytes")
			w.Header().Add("Content-Length", fmt.Sprintf("%d", fileSize))
			w.WriteHeader(200)
		} else if r.Method == "GET" {
			log.WithFields(log.Fields{
				"header": r.Header,
			}).Info("received GET request")
			w.WriteHeader(http.StatusPartialContent)
			chunkRange := strings.Split(strings.Split(r.Header.Get("range"), "bytes=")[1], "-")
			start, err := strconv.Atoi(chunkRange[0])
			if err != nil {
				t.Fatal("error:", err)
			}
			end, err := strconv.Atoi(chunkRange[1])
			if err != nil {
				t.Fatal("error:", err)
			}
			length := end - start
			log.WithFields(log.Fields{
				"start":  start,
				"end":    end,
				"length": length,
			}).Info("parsed range header")
			io.Copy(w, bytes.NewReader(fileToDownload[start:end+1]))
		}
	}))
	defer ts.Close()

	targetURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("error re-parsing url: %v", err)
	}

	outFile := NewFakeWriterAt(fileSize)

	err = Download(*targetURL, outFile)

	if err != nil {
		t.Fatalf("error downloading file: %v", err)
	}

	if len(outFile.d) != len(fileToDownload) {
		t.Fatal("outfile len didn't match file to download len\n", len(outFile.d), "\n", len(fileToDownload))
	}
	for i, b := range outFile.d {
		if b != fileToDownload[i] {
			t.Fatal("outfile didn't match file to download", outFile, fileToDownload)
		}
	}
}
