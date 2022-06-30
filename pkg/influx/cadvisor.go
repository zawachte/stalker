package influx

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	info "github.com/google/cadvisor/info/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"

	influxdb_client_go "github.com/influxdata/influxdb-client-go/v2"
)

var argDbRetentionPolicy = flag.String("storage_driver_influxdb_retention_policy", "", "retention policy")

type CAdvisorClient struct {
	machineName    string
	client         influxdb_client_go.Client
	lastWrite      time.Time
	org            string
	bucket         string
	points         []*write.Point
	lock           sync.Mutex
	bufferDuration time.Duration
	readyToFlush   func() bool
}

// Series names
const (
	// Cumulative CPU usage
	serCPUUsageTotal  string = "cpu_usage_total"
	serCPUUsageSystem string = "cpu_usage_system"
	serCPUUsageUser   string = "cpu_usage_user"
	serCPUUsagePerCPU string = "cpu_usage_per_cpu"
	// Smoothed average of number of runnable threads x 1000.
	serLoadAverage string = "load_average"
	// Memory Usage
	serMemoryUsage string = "memory_usage"
	// Maximum memory usage recorded
	serMemoryMaxUsage string = "memory_max_usage"
	// //Number of bytes of page cache memory
	serMemoryCache string = "memory_cache"
	// Size of RSS
	serMemoryRss string = "memory_rss"
	// Container swap usage
	serMemorySwap string = "memory_swap"
	// Size of memory mapped files in bytes
	serMemoryMappedFile string = "memory_mapped_file"
	// Working set size
	serMemoryWorkingSet string = "memory_working_set"
	// Number of memory usage hits limits
	serMemoryFailcnt string = "memory_failcnt"
	// Cumulative count of memory allocation failures
	serMemoryFailure string = "memory_failure"
	// Cumulative count of bytes received.
	serRxBytes string = "rx_bytes"
	// Cumulative count of receive errors encountered.
	serRxErrors string = "rx_errors"
	// Cumulative count of bytes transmitted.
	serTxBytes string = "tx_bytes"
	// Cumulative count of transmit errors encountered.
	serTxErrors string = "tx_errors"
	// Filesystem limit.
	serFsLimit string = "fs_limit"
	// Filesystem usage.
	serFsUsage string = "fs_usage"
	// Hugetlb stat - current res_counter usage for hugetlb
	setHugetlbUsage = "hugetlb_usage"
	// Hugetlb stat - maximum usage ever recorded
	setHugetlbMaxUsage = "hugetlb_max_usage"
	// Hugetlb stat - number of times hugetlb usage allocation failure
	setHugetlbFailcnt = "hugetlb_failcnt"
	// Perf statistics
	serPerfStat = "perf_stat"
	// Referenced memory
	serReferencedMemory = "referenced_memory"
	// Resctrl - Total memory bandwidth
	serResctrlMemoryBandwidthTotal = "resctrl_memory_bandwidth_total"
	// Resctrl - Local memory bandwidth
	serResctrlMemoryBandwidthLocal = "resctrl_memory_bandwidth_local"
	// Resctrl - Last level cache usage
	serResctrlLLCOccupancy = "resctrl_llc_occupancy"
)

type CAdvisorClientParams struct {
	Token string
	Uri   string
}

func New(params CAdvisorClientParams) (*CAdvisorClient, error) {
	return newStorage(params)
}

// Field names
const (
	fieldValue  string = "value"
	fieldType   string = "type"
	fieldDevice string = "device"
)

// Tag names
const (
	tagMachineName   string = "machine"
	tagContainerName string = "container_name"
)

func (s *CAdvisorClient) ContainerFilesystemStatsToPoints(
	cInfo *info.ContainerInfo,
	stats *info.ContainerStats) (points []*write.Point) {

	if !cInfo.Spec.HasFilesystem {
		return nil
	}

	fsStat := stats.Filesystem
	if fsStat == nil {
		return nil
	}
	tagsFsUsage := map[string]string{
		fieldType: "usage",
	}

	fieldsFsUsage := map[string]interface{}{
		fieldValue: fsStat.TotalUsageBytes,
	}

	pointFsUsage := write.NewPoint(serFsUsage, tagsFsUsage, fieldsFsUsage, stats.Timestamp)

	tagsFsLimit := map[string]string{
		fieldDevice: "", //fsStat.Device,
		fieldType:   "limit",
	}
	fieldsFsLimit := map[string]interface{}{
		fieldValue: "", //int64(fsStat.Limit),
	}
	pointFsLimit := write.NewPoint(serFsLimit, tagsFsLimit, fieldsFsLimit, stats.Timestamp)
	points = append(points, pointFsUsage, pointFsLimit)

	s.TagPoints(cInfo, stats, points)

	return points
}

// Set tags and timestamp for all points of the batch.
// Points should inherit the tags that are set for BatchPoints, but that does not seem to work.
func (s *CAdvisorClient) TagPoints(cInfo *info.ContainerInfo, stats *info.ContainerStats, points []*write.Point) {
	// Use container alias if possible
	var containerName string
	if len(cInfo.Spec.Aliases) > 0 {
		containerName = cInfo.Spec.Aliases[0]
	} else {
		containerName = cInfo.Spec.Image
	}

	commonTags := map[string]string{
		tagMachineName:   s.machineName,
		tagContainerName: containerName,
	}
	for i := 0; i < len(points); i++ {
		// merge with existing tags if any
		addTagsToPoint(points[i], commonTags)
		addTagsToPoint(points[i], cInfo.Spec.Labels)
	}
}

// Set tags and timestamp for all points of the batch.
// Points should inherit the tags that are set for BatchPoints, but that does not seem to work.
func (s *CAdvisorClient) DefaultTags(cInfo *info.ContainerInfo, stats *info.ContainerStats) map[string]string {
	// Use container alias if possible
	var containerName string
	if len(cInfo.Spec.Aliases) > 0 {
		containerName = cInfo.Spec.Aliases[0]
	} else {
		containerName = cInfo.Spec.Image
	}

	commonTags := map[string]string{
		tagMachineName:   s.machineName,
		tagContainerName: containerName,
	}

	return commonTags
}

func (s *CAdvisorClient) ContainerStatsToPoints(
	cInfo *info.ContainerInfo,
	stats *info.ContainerStats,
) (points []*write.Point) {

	defaultTags := s.DefaultTags(cInfo, stats)
	// CPU usage: Total usage in nanoseconds
	points = append(points, makePoint(serCPUUsageTotal, defaultTags, stats.Cpu.Usage.Total, stats.Timestamp))

	// CPU usage: Time spend in system space (in nanoseconds)
	points = append(points, makePoint(serCPUUsageSystem, defaultTags, stats.Cpu.Usage.System, stats.Timestamp))

	// CPU usage: Time spent in user space (in nanoseconds)
	points = append(points, makePoint(serCPUUsageUser, defaultTags, stats.Cpu.Usage.User, stats.Timestamp))

	// CPU usage per CPU
	for i := 0; i < len(stats.Cpu.Usage.PerCpu); i++ {

		point := makePoint(serCPUUsagePerCPU, defaultTags, stats.Cpu.Usage.PerCpu[i], stats.Timestamp)
		tags := map[string]string{"instance": fmt.Sprintf("%v", i)}
		addTagsToPoint(point, tags)
		points = append(points, point)
	}

	// Load Average
	points = append(points, makePoint(serLoadAverage, defaultTags, stats.Cpu.LoadAverage, stats.Timestamp))

	// Network Stats
	points = append(points, makePoint(serRxBytes, defaultTags, stats.Network.Interfaces[0].RxBytes, stats.Timestamp))
	points = append(points, makePoint(serRxErrors, defaultTags, stats.Network.Interfaces[0].RxErrors, stats.Timestamp))
	points = append(points, makePoint(serTxBytes, defaultTags, stats.Network.Interfaces[0].TxBytes, stats.Timestamp))
	points = append(points, makePoint(serTxErrors, defaultTags, stats.Network.Interfaces[0].TxErrors, stats.Timestamp))

	// Referenced Memory
	points = append(points, makePoint(serReferencedMemory, defaultTags, stats.ReferencedMemory, stats.Timestamp))

	s.TagPoints(cInfo, stats, points)

	return points
}

func (s *CAdvisorClient) MemoryStatsToPoints(
	cInfo *info.ContainerInfo,
	stats *info.ContainerStats,
) (points []*write.Point) {

	defaultTags := s.DefaultTags(cInfo, stats)

	// Memory Usage
	points = append(points, makePoint(serMemoryUsage, defaultTags, stats.Memory.Usage, stats.Timestamp))
	// Maximum memory usage recorded
	points = append(points, makePoint(serMemoryMaxUsage, defaultTags, stats.Memory.MaxUsage, stats.Timestamp))
	//Number of bytes of page cache memory
	points = append(points, makePoint(serMemoryCache, defaultTags, stats.Memory.Cache, stats.Timestamp))
	// Size of RSS
	points = append(points, makePoint(serMemoryRss, defaultTags, stats.Memory.RSS, stats.Timestamp))
	// Container swap usage
	points = append(points, makePoint(serMemorySwap, defaultTags, stats.Memory.Swap, stats.Timestamp))
	// Size of memory mapped files in bytes
	points = append(points, makePoint(serMemoryMappedFile, defaultTags, stats.Memory.MappedFile, stats.Timestamp))
	// Working Set Size
	points = append(points, makePoint(serMemoryWorkingSet, defaultTags, stats.Memory.WorkingSet, stats.Timestamp))
	// Number of memory usage hits limits
	points = append(points, makePoint(serMemoryFailcnt, defaultTags, stats.Memory.Failcnt, stats.Timestamp))

	// Cumulative count of memory allocation failures
	memoryFailuresTags := map[string]string{
		"failure_type": "pgfault",
		"scope":        "container",
	}
	memoryFailurePoint := makePoint(serMemoryFailure, defaultTags, stats.Memory.ContainerData.Pgfault, stats.Timestamp)
	addTagsToPoint(memoryFailurePoint, memoryFailuresTags)
	points = append(points, memoryFailurePoint)

	memoryFailuresTags["failure_type"] = "pgmajfault"
	memoryFailurePoint = makePoint(serMemoryFailure, defaultTags, stats.Memory.ContainerData.Pgmajfault, stats.Timestamp)
	addTagsToPoint(memoryFailurePoint, memoryFailuresTags)
	points = append(points, memoryFailurePoint)

	memoryFailuresTags["failure_type"] = "pgfault"
	memoryFailuresTags["scope"] = "hierarchical"
	memoryFailurePoint = makePoint(serMemoryFailure, defaultTags, stats.Memory.HierarchicalData.Pgfault, stats.Timestamp)
	addTagsToPoint(memoryFailurePoint, memoryFailuresTags)
	points = append(points, memoryFailurePoint)

	memoryFailuresTags["failure_type"] = "pgmajfault"
	memoryFailurePoint = makePoint(serMemoryFailure, defaultTags, stats.Memory.HierarchicalData.Pgmajfault, stats.Timestamp)
	addTagsToPoint(memoryFailurePoint, memoryFailuresTags)
	points = append(points, memoryFailurePoint)

	return points
}

func (s *CAdvisorClient) HugetlbStatsToPoints(
	cInfo *info.ContainerInfo,
	stats *info.ContainerStats,
) (points []*write.Point) {

	if !cInfo.Spec.HasHugetlb {
		return nil
	}

	hugeTLB := stats.Hugetlb
	defaultTags := s.DefaultTags(cInfo, stats)

	for pageSize, hugetlbStat := range *hugeTLB {
		tags := map[string]string{
			"page_size": pageSize,
		}

		// Hugepage usage
		point := makePoint(setHugetlbUsage, defaultTags, hugetlbStat.Usage, stats.Timestamp)
		addTagsToPoint(point, tags)
		points = append(points, point)

		//Maximum hugepage usage recorded
		point = makePoint(setHugetlbMaxUsage, defaultTags, hugetlbStat.MaxUsage, stats.Timestamp)
		addTagsToPoint(point, tags)
		points = append(points, point)

		// Number of hugepage usage hits limits
		point = makePoint(setHugetlbFailcnt, defaultTags, hugetlbStat.Failcnt, stats.Timestamp)
		addTagsToPoint(point, tags)
		points = append(points, point)
	}

	s.TagPoints(cInfo, stats, points)

	return points
}

func (s *CAdvisorClient) PerfStatsToPoints(
	cInfo *info.ContainerInfo,
	stats *info.ContainerStats,
) (points []*write.Point) {

	defaultTags := s.DefaultTags(cInfo, stats)

	for _, perfStat := range stats.PerfStats {
		point := makePoint(serPerfStat, defaultTags, perfStat.Value, stats.Timestamp)
		tags := map[string]string{
			"cpu":           fmt.Sprintf("%v", perfStat.Cpu),
			"name":          perfStat.Name,
			"scaling_ratio": fmt.Sprintf("%v", perfStat.ScalingRatio),
		}
		addTagsToPoint(point, tags)
		points = append(points, point)
	}

	s.TagPoints(cInfo, stats, points)

	return points
}

func (s *CAdvisorClient) ResctrlStatsToPoints(
	cInfo *info.ContainerInfo,
	stats *info.ContainerStats,
) (points []*write.Point) {

	defaultTags := s.DefaultTags(cInfo, stats)

	// Memory bandwidth
	for nodeID, rdtMemoryBandwidth := range stats.Resctrl.MemoryBandwidth {
		tags := map[string]string{
			"node_id": fmt.Sprintf("%v", nodeID),
		}
		point := makePoint(serResctrlMemoryBandwidthTotal, defaultTags, rdtMemoryBandwidth.TotalBytes, stats.Timestamp)
		addTagsToPoint(point, tags)
		points = append(points, point)

		point = makePoint(serResctrlMemoryBandwidthLocal, defaultTags, rdtMemoryBandwidth.LocalBytes, stats.Timestamp)
		addTagsToPoint(point, tags)
		points = append(points, point)
	}

	// Cache
	for nodeID, rdtCache := range stats.Resctrl.Cache {
		tags := map[string]string{
			"node_id": fmt.Sprintf("%v", nodeID),
		}
		point := makePoint(serResctrlLLCOccupancy, defaultTags, rdtCache.LLCOccupancy, stats.Timestamp)
		addTagsToPoint(point, tags)
		points = append(points, point)
	}

	s.TagPoints(cInfo, stats, points)

	return points
}

func (s *CAdvisorClient) OverrideReadyToFlush(readyToFlush func() bool) {
	s.readyToFlush = readyToFlush
}

func (s *CAdvisorClient) defaultReadyToFlush() bool {
	return time.Since(s.lastWrite) >= s.bufferDuration
}

func (s *CAdvisorClient) AddStats(cInfo *info.ContainerInfo, stats *info.ContainerStats) error {
	if stats == nil {
		return nil
	}
	var pointsToFlush []*write.Point
	func() {
		// AddStats will be invoked simultaneously from multiple threads and only one of them will perform a write.
		s.lock.Lock()
		defer s.lock.Unlock()

		s.points = append(s.points, s.ContainerStatsToPoints(cInfo, stats)...)
		s.points = append(s.points, s.MemoryStatsToPoints(cInfo, stats)...)
		s.points = append(s.points, s.HugetlbStatsToPoints(cInfo, stats)...)
		s.points = append(s.points, s.PerfStatsToPoints(cInfo, stats)...)
		s.points = append(s.points, s.ResctrlStatsToPoints(cInfo, stats)...)
		s.points = append(s.points, s.ContainerFilesystemStatsToPoints(cInfo, stats)...)
		if s.readyToFlush() {
			pointsToFlush = s.points
			s.points = make([]*write.Point, 0)
			s.lastWrite = time.Now()
		}
	}()
	if len(pointsToFlush) > 0 {
		writeAPI := s.client.WriteAPIBlocking(s.org, s.bucket)
		writeAPI.WritePoint(context.TODO(), pointsToFlush...)
	}

	return nil
}

func (s *CAdvisorClient) GetStats() ([]map[string]interface{}, error) {

	queryAPI := s.client.QueryAPI(s.org)

	returnList := []map[string]interface{}{}
	result, err := queryAPI.Query(context.Background(), `from(bucket:"metrics") |> range(start:-1m)`)
	if err == nil {
		// Iterate over query response
		for result.Next() {
			// Notice when group key has changed

			returnList = append(returnList, result.Record().Values())
		}
		// check for an error
		if result.Err() != nil {
			return nil, result.Err()
		}
	} else {
		return nil, err
	}

	return returnList, nil
}

func (s *CAdvisorClient) Close() error {
	s.client = nil
	return nil
}

// machineName: A unique identifier to identify the host that current cAdvisor
// instance is running on.
// influxdbHost: The host which runs influxdb (host:port)
func newStorage(params CAdvisorClientParams) (*CAdvisorClient, error) {

	client := influxdb_client_go.NewClient(params.Uri, params.Token) // "http://localhost:8086", "token")

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	ret := &CAdvisorClient{
		client:      client,
		org:         DefaultOrgName,
		bucket:      DefaultBucketName,
		lastWrite:   time.Now(),
		points:      make([]*write.Point, 0),
		machineName: hostname,
	}
	ret.readyToFlush = ret.defaultReadyToFlush
	return ret, nil
}

// Creates a measurement point with a single value field
func makePoint(name string, tags map[string]string, value interface{}, ts time.Time) *write.Point {

	fields := map[string]interface{}{
		fieldValue: toSignedIfUnsigned(value),
	}

	return write.NewPoint(name, tags, fields, ts)
}

// Adds additional tags to the existing tags of a point
func addTagsToPoint(point *write.Point, tags map[string]string) {
	for k, v := range tags {
		point.AddTag(k, v)
	}
}

// Some stats have type unsigned integer, but the InfluxDB client accepts only signed integers.
func toSignedIfUnsigned(value interface{}) interface{} {
	switch v := value.(type) {
	case uint64:
		return int64(v)
	case uint32:
		return int32(v)
	case uint16:
		return int16(v)
	case uint8:
		return int8(v)
	case uint:
		return int(v)
	}
	return value
}
