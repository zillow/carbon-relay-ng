package main

import (
	"encoding/json"
	"fmt"
	assetfs "github.com/elazarl/go-bindata-assetfs"
	"github.com/gorilla/mux"
	"github.com/graphite-ng/carbon-relay-ng/aggregator"
	"github.com/graphite-ng/carbon-relay-ng/rewriter"
	"net/http"
	"os"
	"strconv"
	"time"
)

// error response contains everything we need to use http.Error
type handlerError struct {
	Error   error
	Message string
	Code    int
}

// a custom type that we can use for handling errors and formatting responses
type handler func(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError)

// attach the standard ServeHTTP method to our handler so the http library can call it
func (fn handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// here we could do some prep work before calling the handler if we wanted to

	// call the actual handler
	response, err := fn(w, r)

	// check for errors
	if err != nil {
		//log.Printf("ERROR: %v\n", err.Error)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Message+": "+err.Error.Error()), err.Code)
		return
	}
	if response == nil {
		//log.Printf("ERROR: response from method is nil\n")
		http.Error(w, "Internal server error. Check the logs.", http.StatusInternalServerError)
		return
	}

	// turn the response into JSON
	bytes, e := json.Marshal(response)
	if e != nil {
		http.Error(w, fmt.Sprintf("Error marshalling JSON:'%s'", e), http.StatusInternalServerError)
		return
	}

	// send the response and log
	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

func showConfig(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	return config, nil
}

func listTable(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	t := table.Snapshot()
	return t, nil
}

func badMetricsHandler(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	timespec := mux.Vars(r)["timespec"]
	duration, err := time.ParseDuration(timespec)
	if err != nil {
		return nil, &handlerError{err, "Could not parse timespec", http.StatusBadRequest}
	}

	records := badMetrics.Get(duration)
	return records, nil
}

func removeRewriter(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	index := mux.Vars(r)["index"]
	idx, _ := strconv.Atoi(index)
	err := table.DelRewriter(idx)
	if err != nil {
		return nil, &handlerError{nil, "Could not find entry " + index, http.StatusNotFound}
	}
	return make(map[string]string), nil
}

func removeBlacklist(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	index := mux.Vars(r)["index"]
	idx, _ := strconv.Atoi(index)
	err := table.DelBlacklist(idx)
	if err != nil {
		return nil, &handlerError{nil, "Could not find entry " + index, http.StatusNotFound}
	}
	return make(map[string]string), nil
}

func removeAggregator(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	index := mux.Vars(r)["index"]
	idx, _ := strconv.Atoi(index)
	err := table.DelAggregator(idx)
	if err != nil {
		return nil, &handlerError{nil, err.Error(), http.StatusNotFound}
	}
	return make(map[string]string), nil
}

func removeDestination(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	key := mux.Vars(r)["key"]
	index := mux.Vars(r)["index"]
	idx, _ := strconv.Atoi(index)
	err := table.DelDestination(key, idx)
	if err != nil {
		return nil, &handlerError{nil, "Could not find entry " + key + "/" + index, http.StatusNotFound}
	}
	return make(map[string]string), nil
}

func listRoutes(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	t := table.Snapshot()
	return t.Routes, nil
}

func getRoute(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	key := mux.Vars(r)["key"]
	route := table.GetRoute(key)
	if route == nil {
		return nil, &handlerError{nil, "Could not find route " + key, http.StatusNotFound}
	}
	return route, nil
}

func removeRoute(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	key := mux.Vars(r)["key"]
	err := table.DelRoute(key)
	if err != nil {
		return nil, &handlerError{nil, "Could not find entry " + key, http.StatusNotFound}
	}
	return make(map[string]string), nil
}
func parseRouteRequest(r *http.Request) (Route, *handlerError) {
	var request struct {
		Address   string
		Key       string
		Pickle    bool
		Spool     bool
		Type      string
		Substring string
		Prefix    string
		Regex     string
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return nil, &handlerError{err, "Couldn't parse json", http.StatusBadRequest}
	}
	// use hard coded defaults for flush and reconn as specified in
	// readDestinations
	periodFlush := time.Duration(1000) * time.Millisecond
	periodReconn := time.Duration(10000) * time.Millisecond
	dest, err := NewDestination("", "", "", request.Address, table.spoolDir, request.Spool, request.Pickle, periodFlush, periodReconn)
	if err != nil {
		return nil, &handlerError{err, "unable to create destination", http.StatusBadRequest}
	}
	var route Route
	switch request.Type {
	case "sendAllMatch":
		route, err = NewRouteSendAllMatch(request.Key, request.Prefix, request.Substring, request.Regex, []*Destination{dest}, make(map[string][]*Destination))
	case "sendFirstMatch":
		route, err = NewRouteSendFirstMatch(request.Key, request.Prefix, request.Substring, request.Regex, []*Destination{dest}, make(map[string][]*Destination))
	default:
		return nil, &handlerError{nil, "unknown route type: " + request.Type, http.StatusBadRequest}
	}
	return route, nil
}
func parseAggregateRequest(r *http.Request) (*aggregator.Aggregator, *handlerError) {
	var request struct {
		Fun      string
		OutFmt   string
		Interval uint
		Wait     uint
		Regex    string
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return nil, &handlerError{err, "Couldn't parse json", http.StatusBadRequest}
	}
	aggregate, err := aggregator.New(request.Fun, request.Regex, request.OutFmt, request.Interval, request.Wait, table.In)
	if err != nil {
		return nil, &handlerError{err, "Couldn't create aggregator", http.StatusBadRequest}
	}
	return aggregate, nil
}
func parseRewriterRequest(r *http.Request) (rewriter.RW, *handlerError) {
	var request struct {
		Old string
		New string
		Max int
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return rewriter.RW{}, &handlerError{err, "Couldn't parse json", http.StatusBadRequest}
	}
	rw, err := rewriter.New(request.Old, request.New, request.Max)
	if err != nil {
		return rewriter.RW{}, &handlerError{err, "Couldn't create rewriter", http.StatusBadRequest}
	}
	return rw, nil
}

/* needs updating, but using what api?
func updateRoute(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	payload, err := parseRouteRequest(r)
	if err != nil {
		return nil, err
	}

	e := routes.Update(payload.Key, &payload.Addr, &payload.Patt)
	if e != nil {
		return nil, &handlerError{e, "Could not update route (" + e.Error() + ")", http.StatusBadRequest}
	}
	return routes.Map[payload.Key], nil
}

*/
func addAggregate(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	aggregate, err := parseAggregateRequest(r)
	if err != nil {
		return nil, err
	}

	table.AddAggregator(aggregate)
	return map[string]string{"Message": "aggregate added"}, nil
}

func addRewrite(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	rw, err := parseRewriterRequest(r)
	if err != nil {
		return nil, err
	}

	table.AddRewriter(rw)
	return map[string]string{"Message": "rewriter added"}, nil
}

func addRoute(w http.ResponseWriter, r *http.Request) (interface{}, *handlerError) {
	route, err := parseRouteRequest(r)
	if err != nil {
		return nil, err
	}

	table.AddRoute(route)
	return map[string]string{"Message": "route added"}, nil
}

func HttpListener(addr string, t *Table) {
	table = t

	router := mux.NewRouter()
	router.Handle("/badMetrics/{timespec}.json", handler(badMetricsHandler)).Methods("GET")
	router.Handle("/config", handler(showConfig)).Methods("GET")
	router.Handle("/table", handler(listTable)).Methods("GET")
	router.Handle("/blacklists/{index}", handler(removeBlacklist)).Methods("DELETE")
	router.Handle("/rewriters/{index}", handler(removeRewriter)).Methods("DELETE")
	router.Handle("/rewriters", handler(addRewrite)).Methods("POST")
	router.Handle("/aggregators/{index}", handler(removeAggregator)).Methods("DELETE")
	router.Handle("/aggregators", handler(addAggregate)).Methods("POST")
	router.Handle("/routes", handler(listRoutes)).Methods("GET")
	router.Handle("/routes", handler(addRoute)).Methods("POST")
	router.Handle("/routes/{key}", handler(getRoute)).Methods("GET")
	//router.Handle("/routes/{key}", handler(updateRoute)).Methods("POST")
	router.Handle("/routes/{key}", handler(removeRoute)).Methods("DELETE")
	router.Handle("/routes/{key}/destinations/{index}", handler(removeDestination)).Methods("DELETE")

	router.PathPrefix("/").Handler(http.FileServer(&assetfs.AssetFS{Asset: Asset, AssetDir: AssetDir, AssetInfo: AssetInfo, Prefix: "admin_http_assets/"}))
	http.Handle("/", router)

	log.Notice("admin HTTP listener starting on %v", addr)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
}
