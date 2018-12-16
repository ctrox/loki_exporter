package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

const namespace = "loki"

// Exporter represents the structur of the exporter
type Exporter struct {
	up           prometheus.Gauge
	totalScrapes prometheus.Counter
	lokiMetrics  map[string]*prometheus.GaugeVec
}

// QueryResponse represents the structure of the response for a loki query request
type QueryResponse struct {
	Streams []struct {
		Labels  string `json:"labels"`
		Entries []struct {
			Timestamp time.Time `json:"timestamp"`
			Line      string    `json:"line"`
		} `json:"entries"`
	} `json:"streams"`
}

// NewExporter returns an initialized exporter
func NewExporter(lokiMetrics map[string]*prometheus.GaugeVec) (*Exporter, error) {
	return &Exporter{
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "success",
			Help:      "Was the last scrape of loki successful.",
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_total_scrapes",
			Help:      "Current total loki scrapes.",
		}),
		lokiMetrics: lokiMetrics,
	}, nil
}

func (e *Exporter) scrape() {
	e.totalScrapes.Inc()
	e.up.Set(0)

	// Queries
	for _, query := range exporterConfig.Queries {
		requestURL := exporterConfig.Loki.ListenAddress + "/api/prom/query?"
		requestURL += "query=" + query.Query
		requestURL += "&limit=" + strconv.FormatInt(query.Limit, 10)
		requestURL += "&start=" + query.Start
		requestURL += "&end=" + query.End
		requestURL += "&direction=" + query.Direction
		requestURL += "&regexp=" + query.Regexp

		log.Debugln(requestURL)

		req, err := http.NewRequest("GET", requestURL, nil)
		if err != nil {
			log.Errorln(err)
			continue
		}

		if exporterConfig.Loki.BasicAuth.Enabled {
			req.SetBasicAuth(exporterConfig.Loki.BasicAuth.Username, exporterConfig.Loki.BasicAuth.Password)
		}

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Errorln(err)
			continue
		}

		if res.StatusCode != 200 {
			log.Errorf("invalid response code: %d", res.StatusCode)
			continue
		}

		result := QueryResponse{}
		json.NewDecoder(res.Body).Decode(&result)
		defer res.Body.Close()

		for index, stream := range result.Streams {
			var labelNames []string
			labels := prometheus.Labels{}
			name := query.Name + strconv.FormatInt(int64(index), 10)

			labelValuePairs := strings.Split(stream.Labels[1:len(stream.Labels)-1], ",")

			for _, labelValuePair := range labelValuePairs {
				labelValuePairSlice := strings.Split(labelValuePair, "=")
				label := labelValuePairSlice[0]
				value := labelValuePairSlice[1]

				labels[strings.Trim(strings.TrimSpace(label), "_")] = strings.TrimSpace(value[1 : len(value)-1])
				labelNames = append(labelNames, strings.Trim(strings.TrimSpace(label), "_"))
			}

			e.lokiMetrics[name] = prometheus.NewGaugeVec(
				prometheus.GaugeOpts{
					Namespace: namespace,
					Name:      query.Name,
					Help:      "number of entries",
				},
				labelNames,
			)

			e.lokiMetrics[name].With(labels).Set(float64(len(stream.Entries)))
		}
	}

	e.up.Set(1)
}

func (e *Exporter) resetMetrics() {
	for _, m := range e.lokiMetrics {
		m.Reset()
	}
}

func (e *Exporter) collectMetrics(metrics chan<- prometheus.Metric) {
	for _, m := range e.lokiMetrics {
		m.Collect(metrics)
	}
}

// Describe describes all the metrics ever exported by the elasticsearch alerts exporter. It implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.up.Desc()
	ch <- e.totalScrapes.Desc()
}

// Collect fetches the stats from configured Elasticsearch location and delivers them as Prometheus metrics. It implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.resetMetrics()
	e.scrape()

	ch <- e.up
	ch <- e.totalScrapes
	e.collectMetrics(ch)
}