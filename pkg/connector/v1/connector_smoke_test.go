package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"testing"
	"time"
)

func TestCanHandleGetRequest(t *testing.T) {
	expect := assert.New(t)

	req, _ := http.NewRequest("GET", testEndpoint("/get"), nil)
	resp, err := testClient.Do(req)

	expect.Nilf(err, "failed to request to cranker: %q", err)

	defer resp.Body.Close()
	expect.Equal("200 OK", resp.Status)
}

func TestCanHandleReconnect(t *testing.T) {
	expect := assert.New(t)

	for i := 0; i < 6; i++ {
		req, _ := http.NewRequest("GET", testEndpoint("/get"), nil)
		resp, err := testClient.Do(req)

		expect.Nilf(err, "failed to request to cranker: %q", err)

		defer resp.Body.Close()
		expect.Equal("200 OK", resp.Status)

		time.Sleep(100 * time.Microsecond)
	}
}

func TestCanHandlePostRequest(t *testing.T) {
	expect := assert.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
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

	expect.Nilf(err, "not a valid httpbin response")
	expect.Equal("hello world", binResp.Data)
}

func TestCanSendCookies(t *testing.T) {
	expect := assert.New(t)
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
