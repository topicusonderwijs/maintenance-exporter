package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/fsnotify/fsnotify"
	"github.com/go-co-op/gocron/v2"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"

	c  Config
	tz *time.Location
)

// ExporterConfig holds the configuration of the Exporter service.
type ExporterConfig struct {
	Addr                 string `yaml:"addr"`
	Timezone             string `yaml:"timezone"`
	LogFormat            string `yaml:"logformat"`
	ExposeProcessMetrics string `yaml:"expose_process_metrics"`
}

// MaintenanceWindowConfig holds the configuration of a single Maintenance
// window.
type MaintenanceWindowConfig struct {
	Name     string            `yaml:"name"`
	Duration string            `yaml:"duration"`
	Labels   map[string]string `yaml:"labels"`
	Cron     string            `yaml:"cron"`
	Timezone string            `yaml:"timezone,omitempty"`
}

// Config describes the yaml configuration file.
type Config struct {
	Config  ExporterConfig            `yaml:"config"`
	Windows []MaintenanceWindowConfig `yaml:"windows"`
}

// MaintenaceWindow is an instance of a Maintenance Window. It holds the:
//   - Duration: How long the maintenance window should stay active after
//     it has been enabled by the scheduler.
//   - Gauge: The actual prometheus metric.
//   - Job: A reference to the Job as it is instantiated in by the scheduler.
type MaintenanceWindow struct {
	Name           string
	Labels         map[string]string
	CronExpression string
	Duration       time.Duration
	Job            gocron.Job
	Gauge          *metrics.Gauge
	gaugeValue     float64
	tz             *time.Location
}

// Task is the function that sets the metric to 1 en resets it to 0 once the
// duration has elapsed.
func (m *MaintenanceWindow) Task() {

	endTime := time.Now().Add(m.Duration)
	msg := fmt.Sprintf("Maintenance Window Open: \"%v\", Closing at: %v ",
		m.Name, endTime.In(tz).Format("2006-01-02 15:04:05"))
	log.Println(msg)
	m.setActive()
	select {
	case <-time.After(m.Duration):
		nextRunTime, err := m.Job.NextRun()
		if err != nil {
			log.Printf("ERROR: Could not retrieve next runtime: %v", err)
		}

		log.Printf("Maintenance Window Closed: \"%v\" Next run: %v",
			m.Name, nextRunTime.In(tz).Format("2006-01-02 15:04:05"))
		m.setInactive()
	}

}

// String method return a string representation of the Maintenance window.
func (m *MaintenanceWindow) String() string {
	msg := fmt.Sprintf("\"%v\"(%v) - {", m.Name, m.CronExpression)
	count := 0
	for k, v := range m.Labels {
		msg += fmt.Sprintf("%v:\"%v\"", k, v)
		if count == len(m.Labels)-1 {
			msg += fmt.Sprintf("}")
		} else {
			msg += fmt.Sprintf(",")
		}
		count++
	}

	return msg
}

func (m *MaintenanceWindow) setActive() {
	log.Tracef("setActive")
	m.gaugeValue = float64(1)
}

func (m *MaintenanceWindow) setInactive() {
	log.Tracef("setInactive")
	m.gaugeValue = float64(0)
}

func (m *MaintenanceWindow) getGaugeValue() float64 {
	log.Tracef("getGaugeValue")
	return m.gaugeValue
}

// NewMaintenanceWindow instantiates a MaintenanceWindow from string values. The
// string values are parsed to the according types.
func NewMaintenanceWindow(
	s gocron.Scheduler, c, d, n, cfgTz string, l map[string]string) (*MaintenanceWindow, error) {

	// add the "name" from the maintenance window configuration to the metrics
	// labelset.
	l["name"] = n

	var err error
	var m MaintenanceWindow

	m.Name = n
	m.Labels = l
	m.CronExpression = c
	m.tz = tz
	if cfgTz != "" {
		m.tz, err = time.LoadLocation(cfgTz)
		if err != nil {
			log.Fatal(err)
		}
	}
	m.Labels["configured_timezone"] = m.tz.String()

	// construct the gauge name:
	// maintenance_active{name="asdfasdf",label_a="value_a",...}
	mname := fmt.Sprintf("maintenance_active{")
	count := 0
	for k, v := range l {
		count++
		mname += fmt.Sprintf("%v=\"%v\"", k, v)
		if count == len(l) {
			mname += fmt.Sprintf("}")
		} else {
			mname += fmt.Sprintf(",")
		}
	}

	m.setInactive()
	m.Gauge = metrics.NewGauge(mname, m.getGaugeValue)

	m.Duration, err = time.ParseDuration(d)
	if err != nil {
		log.Printf("ERROR: Failed to parse duration: %v\n", err)
		return nil, err
	}

	cronString := fmt.Sprintf("CRON_TZ=%v %v", m.tz.String(), c)
	jobDef := gocron.CronJob(cronString, true)
	task := gocron.NewTask(m.Task)
	job, err := s.NewJob(jobDef, task)
	if err != nil {
		log.Fatalf("Could not create cronjob \"%v\": %v", m.Name, err)
	}
	nextRunTime, err := job.NextRun()
	if err != nil {
		log.Printf("ERROR: could not obtain next run time for job: %v: %v", job.ID(), err)
		return nil, err
	}
	log.Tracef("Scheduled job: %v, with id: %v, next run: %v", m.Name, job.ID(), nextRunTime)
	m.Job = job

	return &m, err

}

func init() {
	viper.SetConfigName("config")                      // name of config file (without extension)
	viper.AddConfigPath("/etc/maintenance-exporter")   // path to look for the config file in
	viper.AddConfigPath("$HOME/.maintenance-exporter") // call multiple times to add many search paths
	viper.AddConfigPath(".")                           // optionally look for config in the working directory

	viper.SetDefault("Config.Addr", ":9099")
	viper.SetDefault("Config.Timezone", "UTC")
	viper.SetDefault("Config.LogFormat", "text")
	viper.SetDefault("Config.ExposeProcessMetrics", false)
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		panic(fmt.Errorf("Fatal error config file: %w \n", err))
	}

	err = viper.Unmarshal(&c)
	if err != nil {
		log.Fatal(err)
	}

	// TODO: Add logic to gracefully reload the service.
	viper.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("Config file changed:", e.Name)
	})
	viper.WatchConfig()

	if c.Config.LogFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	}

	log.SetReportCaller(true)

	tz, err = time.LoadLocation(c.Config.Timezone)
	if err != nil {
		log.Fatal(err)
	}

	log.SetLevel(log.InfoLevel)

}

func main() {
	log.Printf("maintenance-exporter, version %s (commit %s, built at %s)\n", version, commit, date)

	s, err := gocron.NewScheduler(gocron.WithLocation(tz))
	if err != nil {
		log.Fatalf("Could not create gocron scheduler: %v", err)
	}

	var maintenanceWindows []*MaintenanceWindow
	for _, w := range c.Windows {

		m, err := NewMaintenanceWindow(
			s,
			w.Cron,
			w.Duration,
			w.Name,
			w.Timezone,
			w.Labels,
		)
		if err != nil {
			log.Printf("Failed to parse maintenance window: %v\n", err)
		}

		log.Printf("Loaded: %v", m)
		maintenanceWindows = append(maintenanceWindows, m)
	}

	log.Println("Starting the scheduler...")
	s.Start()

	log.Printf("-----------------------------------------------")
	for _, m := range maintenanceWindows {
		nextRun, err := m.Job.NextRun()
		if err != nil {
			log.Fatalf("ERROR: Nextrun: %v, %v", m.Name, err)
		}
		msg := fmt.Sprintf("\"%v\" Nextrun: %v (%v)", m.Name, nextRun.In(tz).
			Format("2006-01-02 15:04:05"), tz.String())

		if m.tz != tz {
			msg += fmt.Sprintf(" / %v(%v)", nextRun.In(m.tz).
				Format("2006-01-02 15:04:05"), m.tz.String())
		}

		log.Printf(msg)
	}
	log.Printf("-----------------------------------------------")

	log.Printf("Start serving metrics on %v/metrics", c.Config.Addr)
	log.Printf("Start serving readiness on %v/readiness", c.Config.Addr)
	log.Printf("Start serving liveness on %v/liveness", c.Config.Addr)

	http.HandleFunc("/metrics", func(w http.ResponseWriter, req *http.Request) {
		//TODO: make boolean toggleble from config
		metrics.WritePrometheus(w, false)
	})

	http.HandleFunc("/liveness", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "I am alive, please don't kill me...")
	})

	http.HandleFunc("/readiness", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "I am ready, please send me some requests...")
	})

	http.ListenAndServe(c.Config.Addr, nil)
}
