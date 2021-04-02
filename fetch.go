package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"eaglesong.dev/dvoice"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var baseURLs = map[string]string{
	"dota2": "https://dota2.fandom.com/wiki/",
	"tf2":   "https://wiki.teamfortress.com/wiki/",
}

var (
	transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          1,
		IdleConnTimeout:       10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	cli = &http.Client{Transport: transport}
)

func grab(url string) (blob []byte, basename string, err error) {
	log.Printf("Fetching %s", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}
	resp, err := cli.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = fmt.Errorf("HTTP %s fetching %s", resp.Status, resp.Request.URL)
		return
	}
	basename = path.Base(resp.Request.URL.Path)
	blob, err = ioutil.ReadAll(resp.Body)
	return
}

func hrefFrom(tok html.Token, attrName string) *url.URL {
	for _, attr := range tok.Attr {
		if attr.Key == attrName {
			u, _ := url.Parse(attr.Val)
			return u
		}
	}
	return nil
}

func wikiURL(baseURL, title string) string {
	if title == "" {
		return ""
	}
	title = strings.ToUpper(title[:1]) + title[1:]
	title = strings.ReplaceAll(title, " ", "_")
	return baseURL + title
}

func findMedia(baseURL, filename string) (string, error) {
	uppercased := strings.ToUpper(filename[:1]) + filename[1:]
	fileURL := baseURL + "File:" + uppercased
	page, actualPageFilename, err := grab(fileURL)
	if err != nil {
		return "", err
	}
	actualPageFilename = strings.TrimPrefix(actualPageFilename, "File:")
	baseU, _ := url.Parse(baseURL)
	var maybeU *url.URL
	t := html.NewTokenizer(bytes.NewReader(page))
	// look for <a href="filename.mp3">filename.mp3</a>
	for t.Next() != html.ErrorToken {
		tok := t.Token()
		switch tok.Type {
		case html.StartTagToken:
			switch tok.DataAtom {
			case atom.A:
				// found a link, remember the href and then look at the link text
				maybeU = hrefFrom(tok, "href")
			default:
				maybeU = nil
			}
		case html.TextToken:
			if maybeU != nil && strings.EqualFold(tok.Data, actualPageFilename) {
				// link text matches wanted filename, use the href from the link
				return baseU.ResolveReference(maybeU).String(), nil
			}
			maybeU = nil
		default:
			maybeU = nil
		}
	}
	return "", fmt.Errorf("no audio tag found in %s", fileURL)
}

func fetchSound(baseURL, filename string) ([][]byte, error) {
	cachePath := filepath.Join("cache", filename+".opus")
	f, err := os.Open(cachePath)
	if err == nil {
		defer f.Close()
		d := gob.NewDecoder(f)
		var frameList [][]byte
		if err := d.Decode(&frameList); err != nil {
			return nil, err
		}
		return frameList, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	mp3URL, err := findMedia(baseURL, filename)
	if err != nil {
		return nil, err
	}
	mp3, _, err := grab(mp3URL)
	if err != nil {
		return nil, err
	}

	var frameList [][]byte
	ch := make(chan []byte)
	done := make(chan struct{})
	go func() {
		for frame := range ch {
			frameList = append(frameList, frame)
		}
		close(done)
	}()
	err = dvoice.Convert(context.Background(), ch, bytes.NewReader(mp3), bitrate)
	close(ch)
	<-done
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(path.Dir(cachePath), 0755); err != nil {
		return nil, err
	}
	f, err = os.Create(cachePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	g := gob.NewEncoder(f)
	if err := g.Encode(frameList); err != nil {
		return nil, err
	}
	return frameList, err
}
