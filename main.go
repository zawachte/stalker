package main

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"flag"

	"github.com/spf13/pflag"
	"github.com/zawachte/stalker/internal/api"
	"github.com/zawachte/stalker/internal/models"
	"github.com/zawachte/stalker/internal/providers"
	"github.com/zawachte/stalker/pkg/influx"
	"github.com/zawachte/stalker/pkg/influx_cli"
	"github.com/zawachte/stalker/pkg/influxd"
	"github.com/zawachte/stalker/pkg/responsewriter"
)

func main() {
	var retention time.Duration
	var backupFrequency time.Duration
	var metricsScrapeFrequency time.Duration

	var unixSocket string
	var backupPath string

	fs := pflag.CommandLine
	fs.DurationVar(&retention,
		"retention",
		time.Hour,
		"retention time for stored metrics",
	)
	fs.DurationVar(&backupFrequency,
		"backup-frequency",
		time.Hour,
		"period for creating database backups",
	)
	fs.DurationVar(&metricsScrapeFrequency,
		"metrics-scrape-frequency",
		time.Second*20,
		"period for creating database backups",
	)
	fs.StringVar(&unixSocket,
		"unix-socket",
		"stalker.sock",
		"unix socket path",
	)

	fs.StringVar(&backupPath,
		"backup-path",
		".",
		"path for database backups",
	)

	fs.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	abortCh := make(chan error, 1)
	go func() {
		err := influxd.RunInfluxD(abortCh)
		if err != nil {
			panic(err)
		}
	}()

	influxd.WaitForInfluxDReady()

	fmt.Println("about to setup")

	influxCli, err := influx_cli.NewClient()
	if err != nil {
		panic(err)
	}

	token := generateToken()
	password := generateToken()

	params := influx_cli.SetupInfluxParams{
		Username:  influx.DefaultUsername,
		Password:  password,
		AuthToken: token,
		Org:       influx.DefaultOrgName,
		Bucket:    influx.DefaultBucketName,
		Retention: retention.String(),
	}

	err = influxCli.SetupInflux(params)
	if err != nil {
		panic(err)
	}

	fmt.Println("setup")
	syscall.Unlink(unixSocket)

	// remove when we get there
	unixListener, err := net.Listen("unix", unixSocket)
	if err != nil {
		panic(err)
	}
	defer unixListener.Close()

	metricsProvider, err := providers.NewProvider(context.Background(), providers.ProviderParams{
		DatabaseUrl:   "http://localhost:8086",
		DatabaseToken: token,
	})
	if err != nil {
		panic(err)
	}

	backupParams := influx_cli.BackupInfluxParams{
		Org:    influx.DefaultOrgName,
		Bucket: influx.DefaultBucketName,
		Path:   backupPath,
	}

	go func() {
		for {
			time.Sleep(backupFrequency)
			err = influxCli.BackupInflux(backupParams)
			if err != nil {
				panic(err)
			}
		}
	}()

	go func() {
		for {
			time.Sleep(metricsScrapeFrequency)
			w := responsewriter.NewCustomResponseWriter()
			r := &http.Request{}
			metricsProvider.GetMetricsList(w, r, models.GetMetricsListParams{})
		}
	}()

	server := http.Server{Handler: api.Handler(metricsProvider)}
	server.Serve(unixListener)
}

func generateToken() string {
	rand.Seed(time.Now().UnixNano())
	chars := []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZÅÄÖ" +
		"abcdefghijklmnopqrstuvwxyzåäö" +
		"0123456789")
	length := 8
	var b strings.Builder
	for i := 0; i < length; i++ {
		b.WriteRune(chars[rand.Intn(len(chars))])
	}

	return b.String()
}
