package route

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lomik/go-carbon/persister"
	"gopkg.in/raintank/schema.v1"
)

func getSchemas(file string) (persister.WhisperSchemas, error) {
	schemas, err := persister.ReadWhisperSchemas(file)
	if err != nil {
		return nil, err
	}
	var defaultFound bool
	for _, schema := range schemas {
		if schema.Pattern.String() == ".*" {
			defaultFound = true
		}
		if len(schema.Retentions) == 0 {
			return nil, fmt.Errorf("retention setting cannot be empty")
		}
	}
	if !defaultFound {
		// good graphite health (not sure what graphite does if there's no .*
		// but we definitely need to always be able to determine which interval to use
		return nil, fmt.Errorf("storage-conf does not have a default '.*' pattern")
	}
	return schemas, nil
}

// parseMetric parses a buffer into a MetricData message, using the schemas to deduce the interval of the data.
// The given orgId will be applied to the MetricData, but note:
// * when sending to api endpoint for hosted metrics (grafanaNet route), tsdb-gw will adjust orgId based on the apiKey used for authentication. Unless it runs in admin mode.
// * kafka-mdm route doesn't authenticate and just uses whatever OrgId is specified
func parseMetric(buf []byte, schemas persister.WhisperSchemas, orgId int) (*schema.MetricData, error) {
	errFmt3Fields := "%q: need 3 fields"
	errFmt := "%q: %s"

	msg := strings.TrimSpace(string(buf))

	elements := strings.Fields(msg)
	if len(elements) != 3 {
		return nil, fmt.Errorf(errFmt3Fields, msg)
	}

	val, err := strconv.ParseFloat(elements[1], 64)
	if err != nil {
		return nil, fmt.Errorf(errFmt, msg, err)
	}

	timestamp, err := strconv.ParseUint(elements[2], 10, 32)
	if err != nil {
		return nil, fmt.Errorf(errFmt, msg, err)
	}

	nameWithTags := elements[0]
	elements = strings.Split(nameWithTags, ";")
	name := elements[0]
	tags := elements[1:]
	sort.Strings(tags)
	nameWithTags = fmt.Sprintf("%s;%s", name, strings.Join(tags, ";"))
	s, ok := schemas.Match(nameWithTags)
	if !ok {
		panic(fmt.Errorf("couldn't find a schema for %q - this is impossible since we asserted there was a default with patt .*", name))
	}

	md := schema.MetricData{
		Name:     name,
		Metric:   name,
		Interval: s.Retentions[0].SecondsPerPoint(),
		Value:    val,
		Unit:     "unknown",
		Time:     int64(timestamp),
		Mtype:    "gauge",
		Tags:     tags,
		OrgId:    orgId,
	}
	return &md, nil
}
