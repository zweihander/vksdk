package packer

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/SevereCloud/vksdk/v2/api"
	"github.com/SevereCloud/vksdk/v2/object"
)

// ErrNoResponse error.
var ErrNoResponse = fmt.Errorf("packer: no response")

type request struct {
	method   string
	params   []api.Params
	callback func(api.Response, error)
}

type batch map[string]request

func (b batch) appendRequest(req request) {
	b["r"+strconv.Itoa(len(b))] = req
}

func (b batch) code() string {
	var sb strings.Builder

	sb.WriteString("return {")

	for id, request := range b {
		sb.WriteString(`"` + id + `":API.` + request.method + "({")

		iterateAll(func(key string, value interface{}) {
			if key == "access_token" ||
				key == "v" ||
				(len(key) > 0 && key[0] == ':') {
				return
			}

			sb.WriteString(strconv.Quote(key) + `:` + strconv.Quote(api.FmtValue(value, 0)) + ",")
		}, request.params...)

		sb.WriteString("}),")
	}

	sb.WriteString("};")

	return sb.String()
}

func (p *Packer) sendBatch(bat batch) {
	if err := p.trySendBatch(bat); err != nil {
		for _, request := range bat {
			request.callback(api.Response{}, err)
		}
	}
}

func (p *Packer) trySendBatch(bat batch) error {
	code := bat.code()

	if p.debug {
		log.Printf("packer: batch: code: \n%s\n", code)
	}

	pack, err := p.execute(code)
	if err != nil {
		return err
	}

	failedRequestIndex := 0

	for name, body := range pack.Responses {
		request, ok := bat[name]
		if !ok {
			if p.debug {
				log.Printf("packer: batch: handler %s not registered\n", name)
			}

			continue
		}

		methodResponse := api.Response{
			Response: body,
		}

		if bytes.Equal(body, []byte("false")) {
			methodErr := executeErrorToMethodError(request, pack.ExecuteErrors[failedRequestIndex])
			methodResponse.Error = methodErr
			failedRequestIndex++
		}

		if p.debug {
			log.Printf("packer: batch: call handler %s (method %s): resp: %s, err: %s\n", name, request.method, body, err)
		}

		if methodResponse.Error.Code == api.ErrNoType {
			request.callback(methodResponse, nil)
		} else {
			request.callback(methodResponse, methodResponse.Error)
		}

		delete(bat, name)
	}

	if len(bat) > 0 {
		for _, req := range bat {
			req.callback(api.Response{}, ErrNoResponse)
		}

		bat = nil
	}

	return nil
}

func executeErrorToMethodError(req request, err api.ExecuteError) api.Error {
	params := make([]object.BaseRequestParam, len(req.params))
	iterateAll(func(key string, value interface{}) {
		params = append(params, object.BaseRequestParam{
			Key:   key,
			Value: api.FmtValue(value, 0),
		})
	}, req.params...)

	return api.Error{
		Message:       err.Msg,
		Code:          api.ErrorType(err.Code),
		RequestParams: params,
	}
}
