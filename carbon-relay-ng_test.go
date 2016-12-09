package main

// for now the tests use 10 vals,
// once everything works better and is tweaked, we can use larger amounts

// TODO re-enable the tests

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	logging "github.com/op/go-logging"
)

var packets0A *dummyPackets
var packets1A *dummyPackets
var packets1B *dummyPackets
var packets1C *dummyPackets

var packets3A *dummyPackets
var packets3B *dummyPackets
var packets3C *dummyPackets

var packets4A *dummyPackets
var packets5A *dummyPackets

var packets6A *dummyPackets
var packets6B *dummyPackets
var packets6C *dummyPackets

var metric70 []byte

func init() {
	instance = "test"
	packets0A = NewDummyPackets("0A", 1)
	packets1A = NewDummyPackets("1A", 10)
	packets1B = NewDummyPackets("1B", 10)
	packets1C = NewDummyPackets("1C", 10)
	packets3A = NewDummyPackets("3A", 1000)
	packets3B = NewDummyPackets("3B", 1000)
	packets3C = NewDummyPackets("3C", 1000)
	packets4A = NewDummyPackets("4A", 10000)
	packets5A = NewDummyPackets("5A", 100000)
	packets6A = NewDummyPackets("6A", 1000000)
	//packets6B = NewDummyPackets("6B", 1000000)
	//packets6C = NewDummyPackets("6C", 1000000)
	logging.SetLevel(logging.NOTICE, "carbon-relay-ng")
	metric70 = []byte("abcde_fghij.klmnopqrst.uv_wxyz.1234567890abcdefg 12345.6789 1234567890") // key = 48, val = 10, ts = 10 -> 70
}

func NewTableOrFatal(tb interface{}, spool_dir, cmd string) *Table {
	table = NewTable(spool_dir)
	fatal := func(err error) {
		switch tb.(type) {
		case *testing.T:
			tb.(*testing.T).Fatal(err)
		case *testing.B:
			tb.(*testing.B).Fatal(err)
		}
	}
	if cmd != "" {
		err := applyCommand(table, cmd)
		if err != nil {
			fatal(err)
		}
	}
	return table
}

func (table *Table) ShutdownOrFatal(t *testing.T) {
	err := table.Shutdown()
	if err != nil {
		t.Fatal(err)
	}
}

// TODO verify that the input buffers are not modified by the routing pipeline

//TODO the length of some of those sleeps/timeouts are not satisfactory, we need to do more perf testing and tuning
//TODO get rid of all sleeps, we can do better sync wait constructs

func DisabledTestSinglePointSingleRoute(t *testing.T) {
	tE := NewTestEndpoint(t, ":2005")
	defer tE.Close()
	na := tE.conditionNumAccepts(1)
	ns := tE.conditionNumSeen(1)
	tE.Start()
	table := NewTableOrFatal(t, "", "addRoute sendAllMatch test1  127.0.0.1:2005 flush=10")
	na.Allow(50 * time.Millisecond)
	table.Dispatch(packets0A.Get(0))
	ns.Allow(500 * time.Millisecond)
	tE.SeenThisOrFatal(packets0A.All())
	table.ShutdownOrFatal(t)
	time.Sleep(100 * time.Millisecond) // not sure yet why, but for some reason there's annoying/confusing conn Close() logs still showing up
	// we don't want to mess up the view of the next test
}

func DisabledTest3RangesWith2EndpointAndSpoolInMiddle(t *testing.T) {
	test3RangesWith2EndpointAndSpoolInMiddle(t, 10, 10)
	time.Sleep(100 * time.Millisecond)
	test3RangesWith2EndpointAndSpoolInMiddle(t, 20, 10)
	time.Sleep(100 * time.Millisecond)
	test3RangesWith2EndpointAndSpoolInMiddle(t, 1000, 50)
	time.Sleep(100 * time.Millisecond)
	test3RangesWith2EndpointAndSpoolInMiddle(t, 50, 1000)
	time.Sleep(100 * time.Millisecond)
	test3RangesWith2EndpointAndSpoolInMiddle(t, 1000, 1000)
}

func test3RangesWith2EndpointAndSpoolInMiddle(t *testing.T, reconnMs, flushMs int) {
	spoolDir := "test3RangesWith2EndpointAndSpoolInMiddle"
	os.RemoveAll(spoolDir)
	os.Mkdir(spoolDir, os.ModePerm)
	tEWaits := sync.WaitGroup{} // for when we want to wait on both tE's simultaneously

	log.Notice("##### START STEP 1: two endpoints, each get data #####")
	// UUU -> up-up-up
	// UDU -> up-down-up
	tUUU := NewTestEndpoint(t, ":2005")
	tUDU := NewTestEndpoint(t, ":2006")
	naUUU := tUUU.conditionNumAccepts(1)
	naUDU := tUDU.conditionNumAccepts(1)
	tUUU.Start()
	tUDU.Start()

	// reconnect retry should be quick now, so we can proceed quicker
	// also flushing freq is increased so we don't have to wait as long
	cmd := fmt.Sprintf("addRoute sendAllMatch test1  127.0.0.1:2005 flush=%d  127.0.0.1:2006 spool=true reconn=%d flush=%d", flushMs, reconnMs, flushMs)
	table := NewTableOrFatal(t, spoolDir, cmd)
	fmt.Println(table.Print())
	log.Notice("waiting for both connections to establish")
	naUUU.AllowBG(100*time.Millisecond, &tEWaits)
	naUDU.AllowBG(100*time.Millisecond, &tEWaits)
	tEWaits.Wait()
	// Give some time for unspooled destination to be marked online.
	// Otherwise, the first metric is sometimes dropped.
	time.Sleep(5 * time.Millisecond)
	log.Notice("sending first batch of metrics to table")
	nsUUU := tUUU.conditionNumSeen(1000)
	nsUDU := tUDU.conditionNumSeen(1000)

	for i := 0; i < 1000; i++ {
		table.Dispatch(packets3A.Get(i))
		// give time to write to conn without triggering slow conn (i.e. no faster than 100k/s)
		// note i'm afraid this sleep masks another issue: data can get reordered.
		// if you take this sleep away, and run like so:
		// go test 2>&1 | egrep '(table sending to route|route.*receiving)' | grep -v 2006
		// you should see that data goes through the table in the right order, but the route receives
		// the points in a different order.
		time.Sleep(1 * time.Microsecond)
	}
	log.Notice("waiting for received data")
	nsUUU.AllowBG(1*time.Second, &tEWaits)
	nsUDU.AllowBG(1*time.Second, &tEWaits)
	tEWaits.Wait()
	log.Notice("validating received data")
	tUUU.SeenThisOrFatal(packets3A.All())
	tUDU.SeenThisOrFatal(packets3A.All())

	log.Notice("##### START STEP 2: tUDU (:2006) goes down (outage) & send more data #####")
	// the route will get the redo and flush that to spool
	tUDU.Close()

	log.Notice("sending second batch of metrics to table")
	nsUUU = tUUU.conditionNumSeen(2000)
	for i := 0; i < 1000; i++ {
		table.Dispatch(packets3B.Get(i))
		//checkerUUU <- metricBuf.Bytes()
		// avoid slow conn drops, but also messages like:
		// 18:39:34.858684 ▶ WARN  dest 127.0.0.1:2006 3B.dummyPacket 123 1000000004 nonBlockingSpool -> dropping due to slow spool
		time.Sleep(50 * time.Microsecond) // this suffices on my SSD
	}

	log.Notice("validating received data")
	nsUUU.Allow(1 * time.Second)
	tUUU.SeenThisOrFatal(mergeAll(packets3A.All(), packets3B.All()))

	log.Notice("##### START STEP 3: bring tUDU back up, it should receive all data it missed thanks to the spooling. + send new data #####")
	tUDU = NewTestEndpoint(t, ":2006")
	na := tUDU.conditionNumAccepts(1)
	tUDU.Start()

	log.Notice("waiting for reconnect")
	na.Allow(time.Duration(reconnMs+50) * time.Millisecond)

	log.Notice("sending third batch of metrics to table")
	nsUUU = tUUU.conditionNumSeen(3000)
	// in theory we only need 2000 points here, but because of the redo buffer it should have sent the first points as well
	nsUDU = tUDU.conditionNumSeen(3000)
	for i := 0; i < 1000; i++ {
		table.Dispatch(packets3C.Get(i))
		time.Sleep(50 * time.Microsecond) // see above
	}

	log.Notice("waiting for received data")
	nsUUU.PreferBG(1*time.Second, &tEWaits)
	nsUDU.PreferBG(3*time.Second, &tEWaits)
	tEWaits.Wait()
	log.Notice("validating received data")
	tUUU.SeenThisOrFatal(mergeAll(packets3A.All(), packets3B.All(), packets3C.All()))
	tUDU.SeenThisOrFatal(mergeAll(packets3A.All(), packets3B.All(), packets3C.All()))
	tUUU.Close()
	tUDU.Close()

	table.ShutdownOrFatal(t)
	os.RemoveAll(spoolDir)
}

func DisabledTest2EndpointsUp(t *testing.T) {
	test2Endpoints(t, 10, 10, packets3A)
	time.Sleep(100 * time.Millisecond)
	test2Endpoints(t, 20, 10, packets3A)
	time.Sleep(100 * time.Millisecond)
	test2Endpoints(t, 1000, 50, packets3A)
	time.Sleep(100 * time.Millisecond)
	test2Endpoints(t, 50, 1000, packets3A)
	time.Sleep(100 * time.Millisecond)
	test2Endpoints(t, 1000, 1000, packets3A)
	time.Sleep(100 * time.Millisecond)

	test2Endpoints(t, 10, 10, packets6A)
	time.Sleep(100 * time.Millisecond)
	test2Endpoints(t, 20, 10, packets6A)
	time.Sleep(100 * time.Millisecond)
	test2Endpoints(t, 1000, 50, packets6A)
	time.Sleep(100 * time.Millisecond)
	test2Endpoints(t, 50, 1000, packets6A)
	time.Sleep(100 * time.Millisecond)
	test2Endpoints(t, 1000, 1000, packets6A)

}

func test2Endpoints(t *testing.T, reconnMs, flushMs int, dp *dummyPackets) {
	spoolDir := "test2endp"
	os.RemoveAll(spoolDir)
	os.Mkdir(spoolDir, os.ModePerm)
	tEWaits := sync.WaitGroup{} // for when we want to wait on both tE's simultaneously

	t1 := NewTestEndpoint(t, ":2005")
	t2 := NewTestEndpoint(t, ":2006")
	na1 := t1.conditionNumAccepts(1)
	na2 := t2.conditionNumAccepts(1)
	t1.Start()
	t2.Start()

	// reconnect retry should be quick now, so we can proceed quicker
	// also flushing freq is increased so we don't have to wait as long
	cmd := fmt.Sprintf("addRoute sendAllMatch test1  127.0.0.1:2005 flush=%d  127.0.0.1:2006 spool=true reconn=%d flush=%d", flushMs, reconnMs, flushMs)
	table := NewTableOrFatal(t, spoolDir, cmd)
	fmt.Println(table.Print())
	log.Notice("waiting for both connections to establish")
	na1.AllowBG(100*time.Millisecond, &tEWaits)
	na2.AllowBG(100*time.Millisecond, &tEWaits)
	tEWaits.Wait()
	// Give some time for unspooled destination to be marked online.
	// Otherwise, the first metric is sometimes dropped.
	time.Sleep(5 * time.Millisecond)
	log.Notice("sending metrics to table")
	ns1 := t1.conditionNumSeen(dp.amount)
	ns2 := t2.conditionNumSeen(dp.amount)

	for buf := range dp.All() {
		table.Dispatch(buf)
		// give time to write to conn without triggering slow conn (i.e. no faster than 100k/s)
		// note i'm afraid this sleep masks another issue: data can get reordered.
		// if you take this sleep away, and run like so:
		// go test 2>&1 | egrep '(table sending to route|route.*receiving)' | grep -v 2006
		// you should see that data goes through the table in the right order, but the route receives
		// the points in a different order.
		time.Sleep(100 * time.Nanosecond) // see above
	}
	log.Notice("waiting for received data")
	var sleep time.Duration
	switch dp.amount {
	case 1000:
		sleep = 1 * time.Second
	case 1000000:
		sleep = 20 * time.Second
	}
	ns1.AllowBG(sleep, &tEWaits)
	ns2.AllowBG(sleep, &tEWaits)
	tEWaits.Wait()
	log.Notice("validating received data")
	t1.SeenThisOrFatal(dp.All())
	t2.SeenThisOrFatal(dp.All())

	t1.Close()
	t2.Close()

	table.ShutdownOrFatal(t)
	os.RemoveAll(spoolDir)
}

func DisabledTestConsistentHashing(t *testing.T) {
	log.Notice("##### START STEP 1: three endpoints")
	ports := []string{
		":2005",
		":2006",
		":2007",
	}
	testConsistentHashing(t, ports, func() *Table {
		cmd := "addRoute consistentHashing test  127.0.0.1:2005  127.0.0.1:2006  127.0.0.1:2007"
		return NewTableOrFatal(t, "", cmd)
	})

	// Wait for listen ports to become available again
	time.Sleep(1 * time.Second)

	log.Notice("##### START STEP 2: start with three endpoints and delete one")
	ports = []string{
		":2005",
		":2006",
	}
	testConsistentHashing(t, ports, func() *Table {
		cmd := "addRoute consistentHashing test  127.0.0.1:2005  127.0.0.1:2006  127.0.0.1:2007"
		table := NewTableOrFatal(t, "", cmd)
		table.DelDestination("test", 2)
		return table
	})

	// Wait for listen ports to become available again
	time.Sleep(1 * time.Second)

	log.Notice("##### START STEP 3: start with two destinations and change one")
	ports = []string{
		":2007",
		":2006",
	}
	testConsistentHashing(t, ports, func() *Table {
		cmd := "addRoute consistentHashing test  127.0.0.1:2005  127.0.0.1:2006"
		table := NewTableOrFatal(t, "", cmd)
		applyCommand(table, "modDest test 0 addr=127.0.0.1:2007")
		return table
	})

	table.ShutdownOrFatal(t)
}

func getHasherFromFirstRoute(table *Table) *ConsistentHasher {
	return table.config.Load().(TableConfig).routes[0].(*RouteConsistentHashing).config.Load().(consistentHashingRouteConfig).Hasher
}

func testConsistentHashing(t *testing.T, endpointPorts []string, initTable func() *Table) {
	tEWaits := sync.WaitGroup{} // for when we want to wait on multiple tE's simultaneously
	// The order of endpointPorts is expected to correspond to the order
	// of the destinations in the table returned by initTable.
	endpoints := make([]*TestEndpoint, len(endpointPorts))
	for i := range endpoints {
		endpoints[i] = NewTestEndpoint(t, endpointPorts[i])
		na := endpoints[i].conditionNumAccepts(1)
		endpoints[i].Start()
		na.AllowBG(100*time.Millisecond, &tEWaits)
	}
	// Lazily instantiate the table here to ensure that endpoints are up
	// before the table destinations start trying to connect.
	table := initTable()
	fmt.Println(table.Print())
	log.Notice("waiting for connections to establish")
	tEWaits.Wait()
	// Give some time for unspooled destination to be marked online.
	// Otherwise, the first metric is sometimes dropped.
	time.Sleep(5 * time.Millisecond)

	log.Notice("sending metrics to table")
	packets := packets3A
	endpointCount := make([]int, len(endpoints))
	endpointChannels := make([]chan []byte, len(endpoints))
	for i := range endpointChannels {
		endpointChannels[i] = make(chan []byte, packets.amount)
	}
	hasher := getHasherFromFirstRoute(table)

	for buf := range packets.All() {
		table.Dispatch(buf)
		idx := hasher.GetDestinationIndex(buf)
		endpointCount[idx]++
		endpointChannels[idx] <- buf
		// give time to write to conn without triggering slow conn (i.e. no faster than 100k/s)
		// note i'm afraid this sleep masks another issue: data can get reordered.
		// if you take this sleep away, and run like so:
		// go test 2>&1 | egrep '(table sending to route|route.*receiving)' | grep -v 2006
		// you should see that data goes through the table in the right order, but the route receives
		// the points in a different order.
		time.Sleep(100 * time.Nanosecond) // see above
	}
	for i := range endpointChannels {
		close(endpointChannels[i])
	}
	log.Notice("waiting for received data")
	for i, endpoint := range endpoints {
		ns := endpoint.conditionNumSeen(endpointCount[i])
		ns.AllowBG(1*time.Second, &tEWaits)
	}
	tEWaits.Wait()
	log.Notice("validating received data")
	for i, endpoint := range endpoints {
		endpoint.SeenThisOrFatal(endpointChannels[i])
		endpoint.Close()
	}
}

func TestAddRewrite(t *testing.T) {
	cmd := "addRewriter = _is -1"
	table := NewTableOrFatal(t, "", cmd)
	table.ShutdownOrFatal(t)
}

// i thought conn will drop messages because the tE tcp handler can't keep up.
// but looks like that's not true (anymore?), it just works without having to sleep after dispatch
// also note the dummyPackets uses a channel api which probably causes most of the slowdown
func benchmarkSendAndReceive(b *testing.B, dp *dummyPackets) {
	logging.SetLevel(logging.ERROR, "carbon-relay-ng") // testendpoint sends a warning because it does something bad with conn at end but it's harmless
	tE := NewTestEndpoint(nil, ":2005")
	na := tE.conditionNumAccepts(1)
	tE.Start()
	table = NewTableOrFatal(b, "", "addRoute sendAllMatch test1  127.0.0.1:2005")
	na.Wait()
	// reminder: go benchmark will invoke this with N = 0, then maybe N = 20, then maybe more
	// and the time it prints is function run divided by N, which
	// should be of a more or less stable time, which gets printed
	fmt.Println()
	for i := 0; i < b.N; i++ {
		log.Notice("iteration %d: sending %d metrics", i, dp.amount)
		ns := tE.conditionNumSeen(dp.amount * (i + 1))
		for m := range dp.All() {
			//fmt.Println("dispatching", m)
			//fmt.Printf("dispatching '%s'\n", string(m))
			table.Dispatch(m)
		}
		log.Notice("waiting until all %d messages received", dp.amount*(i+1))
		ns.Wait()
		log.Notice("iteration %d done. received %d metrics (%d total)", i, dp.amount, dp.amount*(i+1))
	}
	log.Notice("received all %d messages. wrapping up benchmark run", dp.amount*b.N)
	err := table.Shutdown()
	if err != nil {
		b.Fatal(err)
	}
	tE.Close()
}

func BenchmarkSendAndReceiveThousand(b *testing.B) {
	benchmarkSendAndReceive(b, packets3A)
}
func BenchmarkSendAndReceiveTenThousand(b *testing.B) {
	benchmarkSendAndReceive(b, packets4A)
}
func BenchmarkSendAndReceiveHundredThousand(b *testing.B) {
	benchmarkSendAndReceive(b, packets5A)
}
func BenchmarkSendAndReceiveMillion(b *testing.B) {
	benchmarkSendAndReceive(b, packets6A)
}

// just dispatch (coming into table), no matching or sending to route
func BenchmarkTableDispatchMillion(b *testing.B) {
	logging.SetLevel(logging.WARNING, "carbon-relay-ng") // don't care about unroutable notices
	table = NewTableOrFatal(b, "", "")
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000000; j++ {
			table.Dispatch(metric70)
		}
	}
}

func BenchmarkMatchPrefixMillion(b *testing.B) {
	matcher, _ := NewMatcher("abcde_fghij.klmnopqrst", "", "")
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000000; j++ {
			matcher.Match(metric70)
		}
	}
}

func BenchmarkMatchSubstrMillion(b *testing.B) {
	matcher, _ := NewMatcher("", "1234567890abc", "")
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000000; j++ {
			matcher.Match(metric70)
		}
	}
}

func BenchmarkMatchRegexMillion(b *testing.B) {
	matcher, _ := NewMatcher("", "", "abcde_(fghij|foo).[^\\.]+.\\.*.\\.*")
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000000; j++ {
			matcher.Match(metric70)
		}
	}
}

// just sending into route, no matching or sending to dest
func BenchmarkRouteDispatchMillion(b *testing.B) {
	route, err := NewRouteSendAllMatch("", "", "", "", make([]*Destination, 0), make(map[string][]*Destination))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000000; j++ {
			route.Dispatch(metric70)
		}
	}
}
