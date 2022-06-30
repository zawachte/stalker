package providers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/zawachte/stalker/internal/models"
	"github.com/zawachte/stalker/internal/services"
)

type Provider interface {
	GetMetricsList(w http.ResponseWriter, r *http.Request, params models.GetMetricsListParams)
}

type ProviderParams struct {
	DatabaseUrl   string
	DatabaseToken string
}

type provider struct {
	cadvisorService services.CAdvisorService
}

func NewProvider(ctx context.Context, params ProviderParams) (*provider, error) {

	cadvisorService, err := services.NewCAdvisorService(ctx, services.CAdvisorServiceParams{
		DatabaseUrl:   params.DatabaseUrl,
		DatabaseToken: params.DatabaseToken,
	})
	if err != nil {
		return nil, err
	}

	return &provider{
		cadvisorService: cadvisorService,
	}, nil
}

func (p *provider) GetMetricsList(w http.ResponseWriter, r *http.Request, params models.GetMetricsListParams) {
	// deal with periods
	metricsList, err := p.cadvisorService.GetMetricsList(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	metricsListJson, err := json.Marshal(metricsList)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(metricsListJson)
}
