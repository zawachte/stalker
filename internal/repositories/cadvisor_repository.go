package repositories

import (
	"context"
	"encoding/json"
	"sync"

	cadvisorapiv2 "github.com/google/cadvisor/info/v2"
	"github.com/zawachte/stalker/internal/models"
	"github.com/zawachte/stalker/pkg/influx"
)

// CAdvisorRepository
type CAdvisorRepository interface {
	PostStats(context.Context, *cadvisorapiv2.ContainerInfo, *cadvisorapiv2.ContainerStats) error
	GetMetricsList(context.Context) (models.MetricsList, error)
}

type CAdvisorRepositoryParams struct {
	DatabaseUrl   string
	DatabaseToken string
}

// NewCAdvisorRepository creates a fleet repository.
func NewCAdvisorRepository(ctx context.Context, params CAdvisorRepositoryParams) (CAdvisorRepository, error) {
	if params.DatabaseUrl == "" {
		return &cadvisorRepositoryMemory{}, nil
	}

	sd, err := influx.New(influx.CAdvisorClientParams{
		Uri:   params.DatabaseUrl,
		Token: params.DatabaseToken,
	})
	if err != nil {
		return nil, err
	}

	return &cadvisorRepositoryInfluxDB{
		cadvisorInfluxClient: sd,
	}, nil
}

type cadvisorRepositoryInfluxDB struct {
	cadvisorInfluxClient *influx.CAdvisorClient
}

func (cr *cadvisorRepositoryInfluxDB) PostStats(ctx context.Context, info *cadvisorapiv2.ContainerInfo, stat *cadvisorapiv2.ContainerStats) error {
	err := cr.cadvisorInfluxClient.AddStats(info, stat)
	if err != nil {
		return err
	}
	return nil
}

func (cr *cadvisorRepositoryInfluxDB) GetMetricsList(ctx context.Context) (models.MetricsList, error) {
	stats, err := cr.cadvisorInfluxClient.GetStats()
	if err != nil {
		return models.MetricsList{}, err
	}

	metrics := []string{}
	for _, stat := range stats {
		statByte, err := json.Marshal(stat)
		if err != nil {
			return models.MetricsList{}, err
		}

		metrics = append(metrics, string(statByte))
	}

	return models.MetricsList{
		Metrics: &metrics,
	}, nil
}

type cadvisorRepositoryMemory struct {
	mu        sync.Mutex
	simpleMap map[int]models.MetricsList
}

func (cr *cadvisorRepositoryMemory) PostStats(ctx context.Context, info *cadvisorapiv2.ContainerInfo, stat *cadvisorapiv2.ContainerStats) error {
	return nil
}

func (cr *cadvisorRepositoryMemory) GetMetricsList(ctx context.Context) (models.MetricsList, error) {
	return models.MetricsList{}, nil
}
