package exprego

import (
	"errors"
	"fmt"
	"strings"
)

type Request struct {
	method       HttpVerb
	path         string
	Slugs        map[string]string
	searchParams map[string]string
	Headers      map[string]string
	Body         []byte            //This may be a picture or any other bytes flow
	Context      map[string]string //Read only property
}

func newRequest() *Request {
	return &Request{
		//Pre-allocate maps to make it easier adding new entries
		Slugs:        make(map[string]string),
		searchParams: make(map[string]string),
		Headers:      make(map[string]string),
	}
}

func (r *Request) setPath(path string) {
	r.path = path
}

func (r *Request) setMethod(method string) {
	r.method = httpVerbfromString(method)
}

func (r *Request) setHeader(name, value string) {
	r.Headers[name] = value
}

func (r *Request) setSearchParam(name, value string) {
	r.searchParams[name] = value
}

func (r *Request) setSlug(name, value string) {
	r.Slugs[name] = value
}

func (r *Request) setBody(body []byte) {
	r.Body = body
}

func (r *Request) injectContext(context map[string]string) {
	r.Context = context
}

func (r *Request) getRequestContext() *map[string]string {
	return &r.Context
}

type Response struct {
	Status  int16
	Body    []byte
	Headers map[string]*Header
}

type Header struct {
	name  string
	value string
}

func newHeader(name, value string) *Header {
	return &Header{name: name, value: value}
}

func (h Header) toString() string {
	return fmt.Sprintf("%s: %s\r\n", h.name, h.value)
}

func newResponse() *Response {
	response := &Response{
		//Avoids the case where the controller does not set the status
		Status: 200,
		//Pre-allocate maps to make it easier adding new entries
		Headers: make(map[string]*Header),
	}
	response.SetContentType(TEXT_PLAIN)
	return response
}

func (r *Response) setHeader(name, value string) {
	r.Headers[name] = newHeader(name, value)
}

func (r *Response) setBody(body []byte) {
	r.Body = body
}
func (r *Response) SetStatus(status int16) {
	r.Status = status
}

func (r *Response) SetContentType(mt MimeType) {
	ct, ok := mimeTypes(mt)
	if !ok {
		ct, _ = mimeTypes(TEXT_PLAIN)
	}
	r.Headers["Content-Type"] = newHeader("Content-Type", ct)
}

func (r *Response) getContentType() Header {
	return *r.Headers["Content-Type"]
}

func (r *Response) calcContentLength() {
	r.setHeader("Content-Length", fmt.Sprintf("%d", len(string(r.Body))))
}

// Linearize the request into a HTTP message
// Server can chose to write it to any place. Even wrap it
// Dunno if its ok, but the linearization is a []byte to handle sending non text based messages (Pdf files, images)
func (res *Response) serialize() (*[]byte, error) {
	//Min buffer for  HTTP/1.1" "
	buffer := make([]byte, 0, 9) //Init with len 0 to avoid any whitespace at the beggining of the HTTP response
	buffer = append(buffer, []byte("HTTP/1.1 ")...)
	// Append status
	statusMsg, ok := statusCodeToMsg(res.Status)
	if !ok {
		return nil, errors.New("Not supported code")
	}
	buffer = append(buffer, []byte(fmt.Sprintf("%d %s\r\n", res.Status, statusMsg))...) //Status is 200 by default. See newReponse
	//Handle headers
	if len(res.Headers) != 0 {
		s := ""
		for h := range res.Headers {
			s += res.Headers[h].toString()
		}
		s += "\r\n"
		buffer = append(buffer, []byte(s)...)
	}

	//handle body
	if len(res.Body) != 0 {
		buffer = append(buffer, res.Body...)
		buffer = append(buffer, []byte("\r\n")...)
	}
	buffer = append(buffer, []byte("\r\n")...) //End of Request
	return &buffer, nil
}
func (req *Request) parseSlugs(route *Route) error {
	if len(route.slugs) != len(strings.Split(req.path, "/")) {
		return errors.New("Didnt match the path with slugs amount.")
	}
	pathElements := strings.Split(req.path, "/")
	for i := range len(route.slugs) {
		slg := route.slugs[i]
		if slg.sType == LITERAL || slg.name == "" {
			// The case / will be ignored
			// The case /[] will be ignored
			// Cases like /[foo]/ will ignore the last one empty
			// The case /[foo]//[bar] will ignore the middle one
			continue
		}
		req.Slugs[slg.name] = pathElements[i]
	}
	return nil
}

func (req *Request) parseHeaders(route *Route) error {
	return nil
}

// TODO: Move to some static structure
func statusCodeToMsg(status int16) (string, bool) {
	m := make(map[int16]string)
	m[100] = "Continue"
	m[101] = "Switching Protocols"
	m[102] = "Processing"
	m[103] = "Early Hints"
	m[200] = "OK"
	m[201] = "Created"
	m[202] = "Accepted"
	m[203] = "Non-Authoritative Information"
	m[204] = "No Content"
	m[205] = "Reset Content"
	m[206] = "Partial Content"
	m[207] = "Multi-Status"
	m[208] = "Already Reported"
	m[226] = "IM Used"
	m[300] = "Multiple Choices"
	m[301] = "Moved Permanently"
	m[302] = "Found"
	m[303] = "See Other"
	m[304] = "Not Modified"
	m[307] = "Temporary Redirect"
	m[308] = "Permanent Redirect"
	m[400] = "Bad Request"
	m[401] = "Unauthorized"
	m[402] = "Payment Required"
	m[403] = "Forbidden"
	m[404] = "Not Found"
	m[405] = "Method Not Allowed"
	m[406] = "Not Acceptable"
	m[407] = "Proxy Authentication Required"
	m[408] = "Request Timeout"
	m[409] = "Conflict"
	m[410] = "Gone"
	m[411] = "Length Required"
	m[412] = "Precondition Failed"
	m[413] = "Content Too Large"
	m[414] = "URI Too Long"
	m[415] = "Unsupported Media Type"
	m[416] = "Range Not Satisfiable"
	m[417] = "Expectation Failed"
	m[418] = "I'm a teapot"
	m[421] = "Misdirected Request"
	m[422] = "Unprocessable Content"
	m[423] = "Locked"
	m[424] = "Failed Dependency"
	m[425] = "Too Early"
	m[426] = "Upgrade Required"
	m[428] = "Precondition Required"
	m[429] = "Too Many Requests"
	m[431] = "Request Header Fields Too Large"
	m[451] = "Unavailable For Legal Reasons"
	m[500] = "Internal Server Error"
	m[501] = "Not Implemented"
	m[502] = "Bad Gateway"
	m[503] = "Service Unavailable"
	m[504] = "Gateway Timeout"
	m[505] = "HTTP Version Not Supported"
	m[506] = "Variant Also Negotiates"
	m[507] = "Insufficient Storage"
	m[508] = "Loop Detected"
	m[510] = "Not Extended"
	m[511] = "Network Authentication Required"
	msg, ok := m[status]
	return msg, ok
}

type MimeType int

const (
	TEXT_PLAIN MimeType = iota
	JSON
	OCTEC_STREAM
)

func mimeTypes(mime MimeType) (string, bool) {
	m := make(map[MimeType]string)
	m[TEXT_PLAIN] = "text/plain"
	m[JSON] = "application/json"
	m[OCTEC_STREAM] = "application/octet-stream"
	s, ok := m[mime]
	return s, ok
}
