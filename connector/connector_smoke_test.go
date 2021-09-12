package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"
)

func TestCanHandleGetRequest(t *testing.T) {
	t.Parallel()
	expect := Expect{t}

	req, _ := http.NewRequest("GET", testEndpoint("/get"), nil)
	resp, err := testClient.Do(req)

	expect.Nilf(err, "failed to request to cranker: %q", err)

	defer resp.Body.Close()
	expect.Equal("200 OK", resp.Status)
}

func TestCanHandleReconnect(t *testing.T) {
	t.Parallel()
	expect := Expect{t}

	for i := 0; i < 60; i++ {
		req, _ := http.NewRequest("GET", testEndpoint("/get"), nil)
		resp, err := testClient.Do(req)

		expect.Nilf(err, "failed to request to cranker: %q", err)

		defer resp.Body.Close()
		expect.Equal("200 OK", resp.Status)

		time.Sleep(100 * time.Microsecond)
	}
}

func TestCanHandlePostRequest(t *testing.T) {
	t.Parallel()
	expect := Expect{t}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequest("POST",
		testEndpoint("/post"),
		bytes.NewBufferString("hello world"))

	req = req.WithContext(ctx)

	resp, err := testClient.Do(req)

	expect.Nilf(err, "failed to request to cranker")
	expect.Equal("200 OK", resp.Status)

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	expect.Nilf(err, "failed to read resp from cranker")

	binResp := HttpBinResp{}
	err = json.Unmarshal(body, &binResp)

	fmt.Println(string(body))
	expect.Nilf(err, "not a valid httpbin response: %q, %s", err, body)
	expect.Equal("hello world", binResp.Data)
}

func TestCanSendCookies(t *testing.T) {
	t.Parallel()
	expect := Expect{t}
	req, err := http.NewRequest("GET", testEndpoint("/cookies"), nil)
	expect.Nil(err)

	req.Header.Set("Cookie", "hello=world; hi=there")

	resp, err := testClient.Do(req)
	expect.Nil(err)
	expect.Equal(200, resp.StatusCode)

	defer resp.Body.Close()

	rawBody, err := ioutil.ReadAll(resp.Body)
	expect.Nil(nil)

	var b map[string]string
	err = json.Unmarshal(rawBody, &b)
	expect.Nil(err)
	expect.Equal("world", b["hello"])
	expect.Equal("there", b["hi"])
}

func TestCanGetGzipData(t *testing.T) {
	t.Parallel()
	expect := Expect{t}

	req, err := http.NewRequest("GET", testEndpoint("/gzip"), nil)
	expect.Nil(err)

	resp, err := testClient.Do(req)
	expect.Nil(err)
	expect.Equal(200, resp.StatusCode)

	defer resp.Body.Close()

	binResp, err := bin(resp.Body)
	expect.Nil(nil)
	expect.Equal("gzip", binResp.Headers.AcceptEncoding[0])
}

func TestCanHandleRedirect(t *testing.T) {
	t.Parallel()
	expect := Expect{t}

	req, err := http.NewRequest("GET", testEndpoint("/redirect-to"), nil)
	expect.Nil(err)

	query := req.URL.Query()
	query.Add("url", testEndpoint("/get"))
	query.Add("status_code", "302")
	req.URL.RawQuery = query.Encode()

	resp, err := testClient.Do(req)
	expect.Nil(err)
	expect.Equal(200, resp.StatusCode)

	defer resp.Body.Close()

	binResp, err := bin(resp.Body)
	expect.Nil(nil)
	expect.Equal("gzip", binResp.Headers.AcceptEncoding[0])
}
