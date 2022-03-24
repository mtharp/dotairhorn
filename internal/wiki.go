package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

func Grab(url string) (blob []byte, err error) {
	log.Printf("Fetching %s", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "dotairhorn/1.0")
	resp, err := cli.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = fmt.Errorf("HTTP %s fetching %s", resp.Status, resp.Request.URL)
		return
	}
	blob, err = io.ReadAll(resp.Body)
	return
}

func PageURL(baseURL, title string) string {
	if title == "" {
		return ""
	}
	title = strings.ToUpper(title[:1]) + title[1:]
	title = strings.ReplaceAll(title, " ", "_")
	baseURL = strings.TrimSuffix(baseURL, "w/")
	return baseURL + "wiki/" + title
}

func MediaURL(baseURL, filename string) (string, error) {
	q := make(url.Values)
	q.Set("action", "query")
	q.Set("format", "json")
	q.Set("formatversion", "2")
	q.Set("prop", "imageinfo")
	q.Set("iiprop", "url")
	q.Set("titles", "File:"+filename)
	blob, err := Grab(baseURL + "api.php?" + q.Encode())
	if err != nil {
		return "", err
	}
	var info mediaInfo
	if err := json.Unmarshal(blob, &info); err != nil {
		return "", err
	}
	if len(info.Errors) != 0 {
		return "", errors.New("wiki error: " + string(info.Errors))
	}
	if len(info.Query.Pages) == 0 || len(info.Query.Pages[0].ImageInfo) == 0 {
		if len(info.Warnings) != 0 {
			return "", errors.New("wiki warning: " + string(info.Warnings))
		}
		return "", errors.New("file has no URL")
	}
	return info.Query.Pages[0].ImageInfo[0].URL, nil
}

type mediaInfo struct {
	Query struct {
		Pages []struct {
			ImageInfo []struct {
				URL string `json:"url"`
			} `json:"imageinfo"`
		} `json:"pages"`
	} `json:"query"`
	Warnings json.RawMessage `json:"warnings"`
	Errors   json.RawMessage `json:"errors"`
}
