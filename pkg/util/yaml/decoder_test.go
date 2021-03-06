/*
Copyright 2014 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package yaml

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"
)

func TestSplitYAMLDocument(t *testing.T) {
	testCases := []struct {
		input  string
		atEOF  bool
		expect string
		adv    int
	}{
		{"foo", true, "foo", 3},
		{"fo", false, "", 0},

		{"---", true, "---", 3},
		{"---\n", true, "---\n", 4},
		{"---\n", false, "", 0},

		{"\n---\n", false, "", 5},
		{"\n---\n", true, "", 5},

		{"abc\n---\ndef", true, "abc", 8},
		{"def", true, "def", 3},
		{"", true, "", 0},
	}
	for i, testCase := range testCases {
		adv, token, err := splitYAMLDocument([]byte(testCase.input), testCase.atEOF)
		if err != nil {
			t.Errorf("%d: unexpected error: %v", i, err)
			continue
		}
		if adv != testCase.adv {
			t.Errorf("%d: advance did not match: %d %d", i, testCase.adv, adv)
		}
		if testCase.expect != string(token) {
			t.Errorf("%d: token did not match: %q %q", i, testCase.expect, string(token))
		}
	}
}

func TestGuessJSON(t *testing.T) {
	if r, isJSON := guessJSONStream(bytes.NewReader([]byte(" \n{}")), 100); !isJSON {
		t.Fatalf("expected stream to be JSON")
	} else {
		b := make([]byte, 30)
		n, err := r.Read(b)
		if err != nil || n != 4 {
			t.Fatalf("unexpected body: %d / %v", n, err)
		}
		if string(b[:n]) != " \n{}" {
			t.Fatalf("unexpected body: %q", string(b[:n]))
		}
	}
}

func TestScanYAML(t *testing.T) {
	s := bufio.NewScanner(bytes.NewReader([]byte(`---
stuff: 1

---       
  `)))
	s.Split(splitYAMLDocument)
	if !s.Scan() {
		t.Fatalf("should have been able to scan")
	}
	t.Logf("scan: %s", s.Text())
	if !s.Scan() {
		t.Fatalf("should have been able to scan")
	}
	t.Logf("scan: %s", s.Text())
	if s.Scan() {
		t.Fatalf("scan should have been done")
	}
	if s.Err() != nil {
		t.Fatalf("err should have been nil: %v", s.Err())
	}
}

func TestDecodeYAML(t *testing.T) {
	s := NewYAMLToJSONDecoder(bytes.NewReader([]byte(`---
stuff: 1

---       
  `)))
	obj := generic{}
	if err := s.Decode(&obj); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fmt.Sprintf("%#v", obj) != `yaml.generic{"stuff":1}` {
		t.Errorf("unexpected object: %#v", obj)
	}
	obj = generic{}
	if err := s.Decode(&obj); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(obj) != 0 {
		t.Fatalf("unexpected object: %#v", obj)
	}
	obj = generic{}
	if err := s.Decode(&obj); err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}
}

type generic map[string]interface{}

func TestYAMLOrJSONDecoder(t *testing.T) {
	testCases := []struct {
		input  string
		buffer int
		isJSON bool
		err    bool
		out    []generic
	}{
		{` {"1":2}{"3":4}`, 2, true, false, []generic{
			{"1": 2},
			{"3": 4},
		}},
		{" \n{}", 3, true, false, []generic{
			{},
		}},
		{" \na: b", 2, false, false, []generic{
			{"a": "b"},
		}},
		{" \n{\"a\": \"b\"}", 2, false, true, []generic{
			{"a": "b"},
		}},
		{" \n{\"a\": \"b\"}", 3, true, false, []generic{
			{"a": "b"},
		}},
		{`   {"a":"b"}`, 100, true, false, []generic{
			{"a": "b"},
		}},
		{"", 1, false, false, []generic{}},
		{"foo: bar\n---\nbaz: biz", 100, false, false, []generic{
			{"foo": "bar"},
			{"baz": "biz"},
		}},
		{"foo: bar\n---\n", 100, false, false, []generic{
			{"foo": "bar"},
		}},
		{"foo: bar\n---", 100, false, false, []generic{
			{"foo": "bar"},
		}},
		{"foo: bar\n--", 100, false, true, []generic{
			{"foo": "bar"},
		}},
		{"foo: bar\n-", 100, false, true, []generic{
			{"foo": "bar"},
		}},
		{"foo: bar\n", 100, false, false, []generic{
			{"foo": "bar"},
		}},
	}
	for i, testCase := range testCases {
		decoder := NewYAMLOrJSONDecoder(bytes.NewReader([]byte(testCase.input)), testCase.buffer)
		objs := []generic{}

		var err error
		for {
			out := make(generic)
			err = decoder.Decode(&out)
			if err != nil {
				break
			}
			objs = append(objs, out)
		}
		if err != io.EOF {
			switch {
			case testCase.err && err == nil:
				t.Errorf("%d: unexpected non-error", i)
				continue
			case !testCase.err && err != nil:
				t.Errorf("%d: unexpected error: %v", i, err)
				continue
			case err != nil:
				continue
			}
		}
		switch decoder.decoder.(type) {
		case *YAMLToJSONDecoder:
			if testCase.isJSON {
				t.Errorf("%d: expected JSON decoder, got YAML", i)
			}
		case *json.Decoder:
			if !testCase.isJSON {
				t.Errorf("%d: expected YAML decoder, got JSON", i)
			}
		}
		if fmt.Sprintf("%#v", testCase.out) != fmt.Sprintf("%#v", objs) {
			t.Errorf("%d: objects were not equal: \n%#v\n%#v", i, testCase.out, objs)
		}
	}
}
