package exprego

import (
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
)

type routeHandler func(*Request, *Response)

// TODO: Handle Non get requests
// TODO: Check if tree based structure is posible to match paths
// The routing algorithm is a conscuence of my free time and
// Silly imagination, didnt check for any better alternative. Probably should
// How does routing works:
// Each new suscribed route handles a specific method to a path
// Route1 handles: GET /<NEW-PATH>/<NP-SLUG> -> handler1
// Each path is splited by the slash, getting a n-length array of parts
// Route1 splited: ["NEW-PATH", "NP-SLUG"] -> len(*) = 2
// Every n-length new path is stored in an array of Routes with the same amount
// of path parts
//
// When the request comes, its slugs are splited from the path and indexed into the map
// A matcher finds the first anwser to the path
// As a consecunece, the first route suscribed that matches is the one repsonding
// i.e
// Route1: GET /PETS
// Route2: GET /<SLUG>
// Request: GET /PETS
// Answer: Route1
//
// Pros: The routing algorithm is simple
// Cons: Is not possible to implement middlewares to bussines related paths

type Router struct {
	paths       map[uint16][]*Route //A map is more eficient if non contiguos nested levels are defined
	notFound    routeHandler
	malformed   routeHandler
	strictSlash bool //Todo: Implement check
}

type HttpVerb int

// TODO: Handle OPTION, HEAD, CONNECT, TRACE
const (
	GET HttpVerb = iota
	POST
	PATCH
	PUT
	DELETE
)

func (h HttpVerb) toString() string {
	switch h {
	case GET:
		return "GET"
	case POST:
		return "POST"
	case PATCH:
		return "PATCH"
	case PUT:
		return "PUT"
	case DELETE:
		return "DELETE"
	}
	return ""
}

func httpVerbfromString(method string) HttpVerb {
	switch method {
	case "GET":
		return GET
	case "POST":
		return POST
	case "PATCH":
		return PATCH
	case "PUT":
		return PUT
	case "DELETE":
		return DELETE
	}
	panic(fmt.Sprintln("NOT SUPPORTED VERB", method))
}

// The specific [METHOD] /{slug}/{:opt} handler
type Route struct {
	method HttpVerb
	path   string
	//If the path is /{slug1}/{slug2} the names are stored in order.
	//Then remapped into the request into Request.slugs with its value
	slugs   []*Slug
	handler *routeHandler
}

type SlugType int

const (
	LITERAL SlugType = iota
	WILDCART
)

// From a path /asd/[fgh]
// The slugs are:
// - asd -> literal
// - fgh -> matcher
type Slug struct {
	sType SlugType
	value string
	name  string
}

func NewRouter() *Router {
	return &Router{
		paths:       make(map[uint16][]*Route),
		notFound:    defaultNotFound,
		malformed:   defaultMalformed,
		strictSlash: true,
	}
}

func newRoute(path string, method HttpVerb, slugNames []string, handler *routeHandler) *Route {
	slgs := make([]*Slug, 0, len(slugNames))
	for i := range len(slugNames) {
		t := getSlugType(slugNames[i])
		slgs = append(slgs, &Slug{
			sType: t,
			value: slugNames[i],
			name:  getSlugName(slugNames[i], t),
		})
	}

	return &Route{
		path:    path,
		method:  method,
		slugs:   slgs,
		handler: handler,
	}
}

func getSlugType(slug string) SlugType {
	if len(slug) == 0 { // case for /
		return LITERAL
	}
	if string(slug[0]) == "[" && string(slug[len(slug)-1]) == "]" {
		return WILDCART
	}

	return LITERAL
}

func getSlugName(value string, sType SlugType) string {
	switch sType {
	case LITERAL:
		return value
	case WILDCART:
		return value[1 : len(value)-1]
	}

	panic(fmt.Sprintln("Non supported slug type", sType))
}

func (s Slug) doesMatch(str string) bool {
	if s.sType == LITERAL {
		return s.value == str
	}
	//WILDCART slugs just match by existing
	return true
}

func defaultNotFound(_ *Request, res *Response) {
	res.SetStatus(404)
}
func defaultMalformed(_ *Request, res *Response) {
	res.SetStatus(400)
}

// TODO: Handle express like middleware chaining
func (r Router) handleRequest(req *Request, res *Response) {

	//TODO: Add to request slug
	routeHandler := r.findHandler(req)
	if routeHandler == nil {
		fmt.Println("Didnt find handler for", req.method, req.path)
		r.notFound(req, res)
		return
	}
	if err := req.parseSlugs(routeHandler); err != nil {
		r.malformed(req, res)
		return
	}

	(*routeHandler.handler)(req, res)
}

func (r Router) findHandler(req *Request) *Route {
	elements := strings.Split(req.path, "/")
	// elements = elements[1:]
	bros, ok := r.paths[uint16(len(elements))]
	if ok && len(bros) != 0 {
		for i := range len(bros) {
			bro := bros[i]
			if len(bro.slugs) != len(elements) {
				continue
			}
			for iEl := range len(elements) {
				if bro.method != req.method {
					break
				}
				if !bro.slugs[iEl].doesMatch(elements[iEl]) {
					//Me fijo en el proximo slug
					break
				}
				//Si matchea, y es el ultimo elemento a chequear. Tengo un match completo
				//Y esta ruta tiene que manejarlo
				//La primera que encuentra lo maneja. Por eso es important el orden
				//En el que se suscriben
				if iEl == len(elements)-1 {
					return bro
				}
			}
		}
	}
	return nil
}

func (r *Router) suscribeNotFound(f routeHandler) {
	r.notFound = f
}

func (r *Router) Suscribe(method HttpVerb, p string, f routeHandler) {
	//TODO: If strictSlash false, remove last /
	elements := strings.Split(p, "/")
	if slices.Index(elements, "[]") != -1 {
		panic(fmt.Sprintf("%s is not a valid path. Dont use empty slugs", p))
	}
	//Find if the path is already suscribed
	//Go to its level and find between the routes one with the same paths
	//Routes are not sorted, linear search is used
	idxLvl := uint16(len(elements))
	siblings := r.paths[idxLvl]
	if siblings != nil && len(siblings) != 0 {
		//Find
		for i := range len(siblings) {
			//If exists
			if siblings[i].path == p && siblings[i].method == method {
				panic(fmt.Sprintf("%s is already suscribed", p))
			}
		}
	}
	fmt.Println("Suscribed: ", p, method, runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name())
	r.paths[idxLvl] = append(r.paths[idxLvl], newRoute(p, method, elements, &f))
}
