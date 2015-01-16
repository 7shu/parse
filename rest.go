package parse

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"
	"time"
)

const (
	ParseVersion       = "1"
	AppIdHeader        = "X-Parse-Application-Id"
	RestKeyHeader      = "X-Parse-REST-API-Key"
	MasterKeyHeader    = "X-Parse-Master-Key"
	SessionTokenHeader = "X-Parse-Session-Token"
	UserAgentHeader    = "User-Agent"
)

var parseHost = "api.parse.com"
var fieldNameCache map[reflect.Type]map[string]string = make(map[reflect.Type]map[string]string)

type requestT interface {
	method() string
	endpoint() (string, error)
	body() (string, error)
	useMasterKey() bool
	session() *sessionT
	contentType() string
}

type ParseError interface {
	error
	Code() int
	Message() string
}

type parseErrorT struct {
	Code    int    `json:"code" parse:"code"`
	Message string `json:"error" parse:"error"`
}

func (e *parseErrorT) Error() string {
	return fmt.Sprintf("error %d - %s", e.Code, e.Message)
}

type clientT struct {
	appId     string
	restKey   string
	masterKey string

	userAgent  string
	httpClient *http.Client
}

var defaultClient *clientT

// Initialize the parse library with your API keys
func Initialize(appId, restKey, masterKey string) {
	defaultClient = &clientT{
		appId:      appId,
		restKey:    restKey,
		masterKey:  masterKey,
		userAgent:  "github.com/kylemcc/parse",
		httpClient: &http.Client{},
	}
}

// Set the timeout for requests to Parse
//
// Returns an error if called before parse.Initialize
func SetHTTPTimeout(t time.Duration) error {
	if defaultClient == nil {
		return errors.New("parse.Initialize must be called before parse.SetHTTPTimeout")
	}

	defaultClient.httpClient.Timeout = t
	return nil
}

// Set the User Agent to be specified for requests against Parse
//
// Returns an error if called before parse.Initialize
func SetUserAgent(ua string) error {
	if defaultClient == nil {
		return errors.New("parse.Initialize must be called before parse.SetUserAgent")
	}

	defaultClient.userAgent = ua
	return nil
}

func (c *clientT) doRequest(op requestT) ([]byte, error) {
	ep, err := op.endpoint()
	if err != nil {
		return nil, err
	}

	method := op.method()
	var body io.Reader
	if method == "POST" || method == "PUT" {
		b, err := op.body()
		if err != nil {
			return nil, err
		}
		body = strings.NewReader(b)
	}

	req, err := http.NewRequest(method, ep, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add(UserAgentHeader, defaultClient.userAgent)
	req.Header.Add(AppIdHeader, defaultClient.appId)
	if op.useMasterKey() && c.masterKey != "" && op.session() == nil {
		req.Header.Add(MasterKeyHeader, c.masterKey)
	} else {
		req.Header.Add(RestKeyHeader, c.restKey)
		if s := op.session(); s != nil {
			req.Header.Add(SessionTokenHeader, s.sessionToken)
		}
	}

	if c := op.contentType(); c != "" {
		req.Header.Add("Content-Type", op.contentType())
	}
	req.Header.Add("Accept-Encoding", "gzip")

	resp, err := defaultClient.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		if r, err := gzip.NewReader(resp.Body); err != nil {
			return nil, err
		} else {
			reader = r
		}
	default:
		reader = resp.Body
	}

	respBody, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	// Error formats are consistent. If the response is an error,
	// return a ParseError
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		ret := parseErrorT{}
		if err := json.Unmarshal(respBody, &ret); err != nil {
			return nil, err
		}
		return nil, &ret
	}

	return respBody, nil
}

func handleResponse(body []byte, dst interface{}) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("v must be a non-nil pointer")
	}

	data := make(map[string]interface{})
	if err := json.Unmarshal(body, &data); err != nil {
		return err
	}

	if c, ok := data["count"]; ok {
		return populateValue(dst, c)
	} else if r, ok := data["results"]; ok {
		if rl, ok := r.([]interface{}); ok && len(rl) == 0 {
			return ErrNoRows
		}

		// Handle query results
		return populateValue(dst, r)
	} else {
		return populateValue(dst, data)
	}
}

func getFields(t reflect.Type, recurse bool) []reflect.StructField {
	fields := make([]reflect.StructField, 0)

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.Struct:
			if recurse {
				fields = append(fields, getFields(ft, recurse)...)
			} else {
				if len(f.PkgPath) == 0 {
					fields = append(fields, f)
				}
			}
		default:
			if len(f.PkgPath) == 0 {
				fields = append(fields, f)
			}
		}
	}

	return fields
}

func getFieldNameMap(v reflect.Value) map[string]string {
	// Get the actual type we care about. Indirect any pointers, and handle
	ind := reflect.Indirect(v)
	t := ind.Type()
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
	}

	if f, ok := fieldNameCache[t]; ok {
		return f
	}

	fields := getFields(t, true)

	fieldMap := make(map[string]string)
	for _, f := range fields {
		if name, _ := parseTag(f.Tag.Get("parse")); name != "" && name != "-" {
			fieldMap[name] = f.Name
		}
	}

	fieldNameCache[t] = fieldMap
	return fieldMap
}

func populateValue(dst interface{}, src interface{}) error {
	dv := reflect.ValueOf(dst)
	dvi := reflect.Indirect(dv)

	sv := reflect.ValueOf(src)
	svi := reflect.Indirect(sv)

	switch dvi.Kind() {
	case reflect.Slice, reflect.Array:
		if sv.Kind() == reflect.Slice || sv.Kind() == reflect.Array {
			dt := dvi.Type().Elem()
			dvi.Set(reflect.MakeSlice(reflect.SliceOf(dt), 0, sv.Len()))
			for i := 0; i < sv.Len(); i++ {
				var newV reflect.Value
				if dt.Kind() == reflect.Ptr {
					newV = reflect.New(dt.Elem())
				} else {
					newV = reflect.New(dt)
				}

				err := populateValue(newV.Interface(), sv.Index(i).Interface())
				if err != nil {
					return err
				}
				if dt.Kind() == reflect.Ptr {
					dvi = reflect.Append(dvi, newV)
				} else {
					dvi = reflect.Append(dvi, reflect.Indirect(newV))
				}
				dv.Elem().Set(dvi)
			}
		} else {
			return fmt.Errorf("expected slice, got %s", sv.Kind())
		}
	case reflect.Struct: // TODO: Handle other Parse object types ?
		if dvi.Type() == reflect.TypeOf(time.Time{}) || dvi.Type() == reflect.TypeOf(Date{}) {
			if s, ok := src.(string); ok {
				if t, err := parseTime(s); err != nil {
					return err
				} else {
					dvi.Set(reflect.ValueOf(t).Convert(dvi.Type()))
				}
			} else if m, ok := src.(map[string]interface{}); ok {
				if t, ok := m["__type"]; ok {
					if t == "Date" {
						if ds, ok := m["iso"]; ok {
							if t, err := parseTime(ds.(string)); err != nil {
								return err
							} else {
								dvi.Set(reflect.ValueOf(t).Convert(dvi.Type()))
							}
						} else {
							return fmt.Errorf("malformed Date type: %v", m)
						}
					} else {
						return fmt.Errorf("expected Date type got %s", t)
					}
				} else {
					return fmt.Errorf("no __type in object: %v", m)
				}
			} else if svi.Type().ConvertibleTo(dvi.Type()) {
				dvi.Set(sv.Convert(dvi.Type()))
			} else {
				return fmt.Errorf("expected string or Date type, got %s", sv.Type())
			}
		} else if svi.Kind() == reflect.Map {
			fieldNameMap := getFieldNameMap(dvi)
			if m, ok := src.(map[string]interface{}); ok {
				if f := dvi.FieldByName("Extra"); f.IsValid() && f.CanSet() && f.IsNil() {
					f.Set(reflect.ValueOf(make(map[string]interface{})))
				}

				for k, v := range m {
					if k == "__type" {
						continue
					}

					if nk, ok := fieldNameMap[k]; ok {
						k = nk
					}

					k = firstToUpper(k)

					if f := dvi.FieldByName(k); f.IsValid() {
						if f.Kind() == reflect.Ptr {
							if f.IsNil() {
								f.Set(reflect.New(f.Type().Elem()))
							}
						}

						fi := reflect.Indirect(f)
						if fi.CanSet() {
							var err error
							if f.Kind() == reflect.Ptr {
								err = populateValue(f.Interface(), v)
							} else {
								fptr := f.Addr()
								err = populateValue(fptr.Interface(), v)
							}
							if err != nil {
								return fmt.Errorf("can not set field %s - %s", k, err)
							}
						}
					} else if f := dvi.FieldByName("Extra"); f.IsValid() && f.Kind() == reflect.Map {
						f.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(v))
					}
				}
			} else {
				return fmt.Errorf("expected map[string]interface{} got %s", sv.Type())
			}
		} else if svi.Type().AssignableTo(dvi.Type()) {
			dvi.Set(svi)
		} else if p, ok := src.(Pointer); ok {
			newv := reflect.New(dvi.Type())
			newvi := reflect.Indirect(newv)
			if f := newvi.FieldByName("Id"); f.CanSet() {
				f.Set(reflect.ValueOf(p.Id))
			}
			return populateValue(dst, newv.Interface())
		} else {
			return fmt.Errorf("expected map, got %s", svi.Kind())
		}
	default:
		if dvi.Kind() == reflect.Ptr {
			if dvi.IsNil() {
				dvi = reflect.New(dvi.Type())
			}

			dvi = dvi.Elem()
		}

		if sv.Type().AssignableTo(dvi.Type()) {
			if dvi.CanSet() {
				dvi.Set(sv)
			}
			return nil
		} else if sv.Type().ConvertibleTo(dvi.Type()) {
			newV := sv.Convert(dvi.Type())
			if dvi.CanSet() {
				dvi.Set(newV)
			}
			return nil
		}
	}

	return nil
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}
