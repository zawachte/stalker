package services

import (
	"context"

	cadvisorapiv2 "github.com/google/cadvisor/info/v2"
	"github.com/zawachte/stalker/internal/models"
	"github.com/zawachte/stalker/internal/repositories"
	"github.com/zawachte/stalker/pkg/cadvisor"
)

// FleetService
type CAdvisorService interface {
	GetMetricsList(context.Context) (models.MetricsList, error)
	GetMetricsListInPeriod(context.Context, int, int) (models.MetricsList, error)
}

type CAdvisorServiceParams struct {
	DatabaseUrl   string
	DatabaseToken string
}

// NewCAdvisorService creates an cadvisor service.
func NewCAdvisorService(ctx context.Context, params CAdvisorServiceParams) (CAdvisorService, error) {
	repo, err := repositories.NewCAdvisorRepository(ctx, repositories.CAdvisorRepositoryParams{
		DatabaseUrl:   params.DatabaseUrl,
		DatabaseToken: params.DatabaseToken,
	})
	if err != nil {
		return nil, err
	}

	imageFsInfoProvider := cadvisor.NewImageFsInfoProvider("")
	cadvisorInterface, err := cadvisor.New(imageFsInfoProvider, "var/lib/kubelet", []string{}, true)
	if err != nil {
		return nil, err
	}

	err = cadvisorInterface.Start()
	if err != nil {
		return nil, err
	}
	return &cadvisorService{
		cadvisorRepository: repo,
		cadvisorInterface:  cadvisorInterface,
	}, nil
}

type cadvisorService struct {
	cadvisorRepository repositories.CAdvisorRepository
	cadvisorInterface  cadvisor.Interface
}

func latestContainerStats(info *cadvisorapiv2.ContainerInfo) (*cadvisorapiv2.ContainerStats, bool) {
	stats := info.Stats
	if len(stats) < 1 {
		return nil, false
	}
	latest := stats[len(stats)-1]
	if latest == nil {
		return nil, false
	}
	return latest, true
}

func (cs *cadvisorService) GetMetricsList(ctx context.Context) (models.MetricsList, error) {

	infos, err := cs.cadvisorInterface.ContainerInfoV2("/", cadvisorapiv2.RequestOptions{
		IdType:    cadvisorapiv2.TypeName,
		Count:     2, // 2 samples are needed to compute "instantaneous" CPU
		Recursive: true,
	})
	if err != nil {
		return models.MetricsList{}, err
	}

	for _, info := range infos {
		stat, ok := latestContainerStats(&info)
		if !ok {
			continue
		}

		err := cs.cadvisorRepository.PostStats(ctx, &info, stat)
		if err != nil {
			return models.MetricsList{}, err
		}
	}

	return cs.cadvisorRepository.GetMetricsList(ctx)
}

func (cs *cadvisorService) GetMetricsListInPeriod(ctx context.Context, startTime, endTime int) (models.MetricsList, error) {
	return models.MetricsList{}, nil
}
