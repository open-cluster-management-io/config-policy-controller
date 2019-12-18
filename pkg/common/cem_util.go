// Licensed Materials - Property of IBM
// (c) Copyright IBM Corporation 2018, 2019. All Rights Reserved.
// Note to U.S. Government Users Restricted Rights:
// Use, duplication or disclosure restricted by GSA ADP Schedule
// Contract with IBM Corp.

package common

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/golang/glog"
)

// CEMEvent defines an event to post to CEM
type CEMEvent struct {
	Resource   Resource `json:"resource"`
	Summary    string   `json:"summary"`
	Severity   string   `json:"severity"`
	Timestamp  string   `json:"submitted"`
	Sender     Sender   `json:"sender"`
	Type       Type     `json:"type"`
	Resolution bool     `json:"resolution"`
}

// Resource defines an event resource
type Resource struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Cluster string `json:"cluster"`
}

// Sender defines an event resource
type Sender struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Cluster string `json:"cluster"`
}

// Type defines the event type
type Type struct {
	StatusOrThreshold string `json:"statusOrThreshold"`
	EventType         string `json:"eventType"`
}

// PostEvent posts an event to the CEM webhook
func PostEvent(url string, payload []byte) (string, error) {

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	restClient := http.Client{
		Timeout:   time.Second * 30,
		Transport: tr,
	}

	reqBody := bytes.NewBuffer(payload)

	req, err := http.NewRequest("POST", url, reqBody)
	if err != nil {
		return "", err
	}

	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")

	res, err := restClient.Do(req)
	if err != nil {
		return "", err
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("RestCall: err=%s msg=%s", err.Error(), body)
	}
	glog.V(5).Infof("PostEvent returned: %s", body)

	return string(body), nil
}
