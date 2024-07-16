package exprego

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
)

const MAX_REQUEST_SIZE = 5.12 * 10e5 //In bytes - 512KB

type Exprego struct {
	port    int
	router  *Router
	context map[string]string //is injected in all the requests to be used. Similar to context or app in feathers
}

func NewExprego() *Exprego {
	return &Exprego{
		port:    80,
		context: make(map[string]string),
	}
}

func (e *Exprego) AddContext(name, value string) {
	e.context[name] = value
}

func parseReqLine(reqLine []byte) ([]string, error) {
	reqLineParts := bytes.Split(reqLine, []byte(" "))
	if len(reqLineParts) != 3 {
		return nil, errors.New("Malformed request from \n")
	}

	method := string(reqLineParts[0])
	path := string(reqLineParts[1])
	httpVersion := string(reqLineParts[2])
	return []string{method, path, httpVersion}, nil
}

func (e Exprego) handleConnection(conn net.Conn) {
	defer conn.Close()
	//For each new connection generate a new Request and New Response
	//The request is completed and sent into the router to be used by the controlers
	//The response comes back modified by the controler, and destructured into the message writen back
	//into the conn

	req := newRequest()
	req.injectContext(e.context)
	res := newResponse()
	// defer conn.Close()
	//Buffer containing TCP payload
	buffer := make([]byte, MAX_REQUEST_SIZE)
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Errorf("Error:", err)
		return
	}
	if n == 0 {
		fmt.Errorf("Empty connection from %s \n", conn.RemoteAddr().String())
		return
	}
	if n < MAX_REQUEST_SIZE {
		tmp := make([]byte, n)
		copy(tmp, buffer[:n])
		buffer = tmp
	}
	//Parse request to obtain method, and path
	reqParts := bytes.Split(buffer, []byte("\r\n"))
	if len(reqParts) == 0 {
		fmt.Errorf("Malformed request from %s \n", conn.RemoteAddr().String())
		//TODO: Return 400?
		return
	}
	/// Starts Req line parsing
	reqLine := reqParts[0]
	parts, err := parseReqLine(reqLine)
	if err != nil {
		fmt.Errorf("%s %s", err.Error(), conn.RemoteAddr().String())
		return
	}

	method, path, httpVersion := parts[0], parts[1], parts[2]
	if httpVersion != "HTTP/1.1" {
		fmt.Errorf("HTTP version", httpVersion, "is not supported")
		return
	}

	req.setPath(path)
	req.setMethod(method)
	///Ends reqline parsing

	///Starts headers parsing
	//The HTTP Message has a whole empty line between reqline+headers and the body
	//https://developer.mozilla.org/en-US/docs/Web/HTTP/Messages#headers
	//So will select all the elements between the reqline and this empty line
	headersDividers := -1
	for i := range reqParts[1:] {
		if i == 0 { //How do i jump the 0? //Todo: Reasign as in body parsing
			continue
		}
		if slices.Equal(reqParts[i], []byte("")) {
			headersDividers = i
			break
		}

		hParts := strings.Split(string(reqParts[i]), ":")
		hName := hParts[0]
		hValue := hParts[1][1:]
		req.setHeader(hName, hValue)
	}
	///Ends headers parsing

	///Starts body parsing
	if len(reqParts) > headersDividers {
		//If there is content after the divider, there is body
		body := reqParts[headersDividers+1:] //Body begins the line after the empty line
		//Will serialize the whole buffer
		bBuffer := make([]byte, 0, len(body[0])) //Pre alloc the first item, it will be at least that long
		for i := range body {
			bBuffer = append(bBuffer, body[i]...)
			// bBuffer = append(bBuffer, []byte("\n")...
		}
		req.Body = bBuffer
	}

	///Ends body parsing
	e.router.handleRequest(req, res)
	//Will compress body content based on request Accept-Encoding header
	if err := compressContent(req, res); err != nil {
		fmt.Sprintf(err.Error())
		conn.Write([]byte("HTTP/1.1 500 Internal Server Error\r\n\r\n"))
		return
	}
	writeBuffer, err := res.serialize()
	if err != nil {
		//TODO: Improve error handling
		fmt.Sprintf(err.Error())
		conn.Write([]byte("HTTP/1.1 400 Malformed request\r\n\r\n"))
		return
	}
	conn.Write(*writeBuffer)
}

func compressContent(req *Request, res *Response) error {
	defer res.calcContentLength() //Will be calculed over the compressed or uncompressed body
	//If the Accept-Encoding header is present, and the value
	//is supported by the server, will compress the body and
	//include the Content-Encoding header to the response
	//If not, ignores the compression and does not inlcude the header
	encoding, ok := req.Headers["Accept-Encoding"]
	if !(ok && isEncodingSupported(encoding)) {
		fmt.Println("DIDNT SUPPORT", ok, isEncodingSupported(encoding))
		return nil
	}

	usedEncoding := selectUsedEncoding(encoding)
	encoder := getEncodingFunction(usedEncoding)
	newBody, err := encoder(&res.Body)
	if err != nil {
		return err
	}
	res.Body = *newBody
	res.calcContentLength()
	res.setHeader("Content-Encoding", usedEncoding)
	return nil
}

// ATM only gzip is supported
func isEncodingSupported(encoding string) bool {
	//The origin may send multiple encodings
	//All are a comma separated string
	//Check if any is supported.
	encodings := strings.SplitAfter(encoding, ", ")
	for i := range encodings {
		//TODO: Include all the possible cases in a map or any other fast lookup structure
		woSpace := strings.TrimSpace(encodings[i])
		e := strings.TrimSuffix(woSpace, ",")
		if "gzip" == e {
			return true
		}
	}

	return false
}

// If gzip is found, will be prioritized
// Otherwise, will use the first one found
func selectUsedEncoding(encodingHeader string) string {
	encodings := strings.SplitAfter(encodingHeader, ",")
	var usedEncoding string
	for i := range encodings {
		//TODO: Include all the possible cases in a map or any other fast lookup structure
		woSpace := strings.TrimSpace(encodings[i])
		e := strings.TrimSuffix(woSpace, ",")
		if "gzip" == e {
			return e
		}
		if usedEncoding == "" {
			usedEncoding = e
		}
	}

	return usedEncoding
}

type Encoder func(*[]byte) (*[]byte, error)

func encodeBody(content *[]byte) (*[]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(*content); err != nil {
		fmt.Println("Zip encoding. Failed to write during compression", err)
		return nil, errors.New("Zip encoding. Failed to compress")
	}
	if err := zw.Flush(); err != nil {
		fmt.Println("Zip encoding. Failed to flush.", err)
		return nil, errors.New("Zip encoding. Failed to flush.")
	}
	if err := zw.Close(); err != nil {
		fmt.Println("Zip encoding. Failed to close writer.", err)
		return nil, errors.New("Zip encoding. Failed to close writer.")
	}
	b := buf.Bytes()
	return &b, nil
}

func getEncodingFunction(usedEncoding string) Encoder {
	if usedEncoding == "gzip" {
		return encodeBody
	}
	return nil
}

func (e *Exprego) Listen() {
	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", e.port))
	if err != nil {
		fmt.Errorf("Failed to bind to port", e.port)
		os.Exit(1)
	}
	fmt.Println("-----------------------------------------")
	fmt.Println("Exprego server started listening at port", e.port)
	fmt.Println("SUSCRIBED", e.amountSuscribed(), "PATH/S")
	fmt.Println("-----------------------------------------")
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Errorf("Error accepting connection: ", err.Error())
			os.Exit(1)
		}

		go e.handleConnection(conn)
	}
}

func (e *Exprego) SetRouter(router *Router) {
	e.router = router
}

func (e *Exprego) SetPort(port int) {
	e.port = port
}

func (e *Exprego) amountSuscribed() int {
	// aux := 0
	// fmt.Println("GEN", len(e.router.paths))
	// for i := range len(e.router.paths) {
	// 	fmt.Println("asd", e.router.paths[uint16(i)], len(e.router.paths[uint16(i)]))
	// 	aux += len(e.router.paths[uint16(i)])
	// }
	return len(e.router.paths)
}
