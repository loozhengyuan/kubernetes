/*
Copyright 2017 The Kubernetes Authors.

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

package diff

import (
	"bytes"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/utils/exec"
)

type FakeObject struct {
	name   string
	merged map[string]interface{}
	live   map[string]interface{}
}

var _ Object = &FakeObject{}

func (f *FakeObject) Name() string {
	return f.name
}

func (f *FakeObject) Merged() (runtime.Object, error) {
	return &unstructured.Unstructured{Object: f.merged}, nil
}

func (f *FakeObject) Live() runtime.Object {
	return &unstructured.Unstructured{Object: f.live}
}

func TestDiffProgram(t *testing.T) {
	externalDiffCommands := [3]string{"diff", "diff -ruN", "diff --report-identical-files"}

	if oriLang := os.Getenv("LANG"); oriLang != "C" {
		os.Setenv("LANG", "C")
		defer os.Setenv("LANG", oriLang)
	}

	for i, c := range externalDiffCommands {
		os.Setenv("KUBECTL_EXTERNAL_DIFF", c)
		streams, _, stdout, _ := genericclioptions.NewTestIOStreams()
		diff := DiffProgram{
			IOStreams: streams,
			Exec:      exec.New(),
		}
		err := diff.Run("/dev/zero", "/dev/zero")
		if err != nil {
			t.Fatal(err)
		}

		// Testing diff --report-identical-files
		if i == 2 {
			output_msg := "Files /dev/zero and /dev/zero are identical\n"
			if output := stdout.String(); output != output_msg {
				t.Fatalf(`stdout = %q, expected = %s"`, output, output_msg)
			}
		}
	}
}

func TestPrinter(t *testing.T) {
	printer := Printer{}

	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"string": "string",
		"list":   []int{1, 2, 3},
		"int":    12,
	}}
	buf := bytes.Buffer{}
	printer.Print(obj, &buf)
	want := `int: 12
list:
- 1
- 2
- 3
string: string
`
	if buf.String() != want {
		t.Errorf("Print() = %q, want %q", buf.String(), want)
	}
}

func TestDiffVersion(t *testing.T) {
	diff, err := NewDiffVersion("MERGED")
	if err != nil {
		t.Fatal(err)
	}
	defer diff.Dir.Delete()

	obj := FakeObject{
		name:   "bla",
		live:   map[string]interface{}{"live": true},
		merged: map[string]interface{}{"merged": true},
	}
	err = diff.Print(&obj, Printer{})
	if err != nil {
		t.Fatal(err)
	}
	fcontent, err := ioutil.ReadFile(path.Join(diff.Dir.Name, obj.Name()))
	if err != nil {
		t.Fatal(err)
	}
	econtent := "merged: true\n"
	if string(fcontent) != econtent {
		t.Fatalf("File has %q, expected %q", string(fcontent), econtent)
	}
}

func TestDirectory(t *testing.T) {
	dir, err := CreateDirectory("prefix")
	defer dir.Delete()
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(dir.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(dir.Name), "prefix") {
		t.Fatalf(`Directory doesn't start with "prefix": %q`, dir.Name)
	}
	entries, err := ioutil.ReadDir(dir.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Directory should be empty, has %d elements", len(entries))
	}
	_, err = dir.NewFile("ONE")
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.NewFile("TWO")
	if err != nil {
		t.Fatal(err)
	}
	entries, err = ioutil.ReadDir(dir.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ReadDir should have two elements, has %d elements", len(entries))
	}
	err = dir.Delete()
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(dir.Name)
	if err == nil {
		t.Fatal("Directory should be gone, still present.")
	}
}

func TestDiffer(t *testing.T) {
	diff, err := NewDiffer("LIVE", "MERGED")
	if err != nil {
		t.Fatal(err)
	}
	defer diff.TearDown()

	obj := FakeObject{
		name:   "bla",
		live:   map[string]interface{}{"live": true},
		merged: map[string]interface{}{"merged": true},
	}
	err = diff.Diff(&obj, Printer{})
	if err != nil {
		t.Fatal(err)
	}
	fcontent, err := ioutil.ReadFile(path.Join(diff.From.Dir.Name, obj.Name()))
	if err != nil {
		t.Fatal(err)
	}
	econtent := "live: true\n"
	if string(fcontent) != econtent {
		t.Fatalf("File has %q, expected %q", string(fcontent), econtent)
	}

	fcontent, err = ioutil.ReadFile(path.Join(diff.To.Dir.Name, obj.Name()))
	if err != nil {
		t.Fatal(err)
	}
	econtent = "merged: true\n"
	if string(fcontent) != econtent {
		t.Fatalf("File has %q, expected %q", string(fcontent), econtent)
	}
}

func TestMask(t *testing.T) {
	cases := []struct {
		name       string
		live       map[string]interface{}
		merged     map[string]interface{}
		wantLive   runtime.Object
		wantMerged runtime.Object
	}{
		{
			name: "v1secret_no_change",
			live: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
					"password": []byte("123"),
				},
			},
			merged: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
					"password": []byte("123"),
				},
			},
			wantLive: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"),
						"password": []byte("***"),
					},
				},
			},
			wantMerged: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"),
						"password": []byte("***"),
					},
				},
			},
		},
		{
			name: "v1secret_object_created",
			live: map[string]interface{}{},
			merged: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
					"password": []byte("123"),
				},
			},
			wantLive: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			wantMerged: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"), // no suffix needed
						"password": []byte("***"), // no suffix needed
					},
				},
			},
		},
		{
			name: "v1secret_object_removed",
			live: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
					"password": []byte("123"),
				},
			},
			merged: map[string]interface{}{},
			wantLive: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"), // no suffix needed
						"password": []byte("***"), // no suffix needed
					},
				},
			},
			wantMerged: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
		},
		{
			name: "v1secret_data_key_added",
			live: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
				},
			},
			merged: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
					"password": []byte("123"), // added
				},
			},
			wantLive: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"),
					},
				},
			},
			wantMerged: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"),
						"password": []byte("***"), // no suffix needed
					},
				},
			},
		},
		{
			name: "v1secret_data_key_changed",
			live: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
					"password": []byte("123"),
				},
			},
			merged: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
					"password": []byte("456"), // changed
				},
			},
			wantLive: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"),
						"password": []byte("*** (before)"), // added suffix for diff
					},
				},
			},
			wantMerged: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"),
						"password": []byte("*** (after)"), // added suffix for diff
					},
				},
			},
		},
		{
			name: "v1secret_data_key_removed",
			live: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
					"password": []byte("123"),
				},
			},
			merged: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"data": map[string][]byte{
					"username": []byte("abc"),
					// "password": []byte("123"), // removed
				},
			},
			wantLive: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"),
						"password": []byte("***"), // no suffix needed
					},
				},
			},
			wantMerged: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"data": map[string][]byte{
						"username": []byte("***"),
						// "password": []byte("***"),
					},
				},
			},
		},
		{
			name: "non_v1secret",
			live: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Namespace",
			},
			merged: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Namespace",
			},
			wantLive: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
				},
			},
			wantMerged: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
				},
			},
		},
	}
	for _, tc := range cases {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			obj := &FakeObject{
				name:   tc.name,
				live:   tc.live,
				merged: tc.merged,
			}
			gotLive, gotMerged, err := Mask(obj)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotLive, tc.wantLive) {
				t.Errorf("live: got: %s, want: %s", gotLive, tc.wantLive)
			}
			if !reflect.DeepEqual(gotMerged, tc.wantMerged) {
				t.Errorf("merged: got: %s, want: %s", gotMerged, tc.wantMerged)
			}
		})
	}
}
