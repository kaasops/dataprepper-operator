// Package kafka provides a Kafka admin client for the Data Prepper operator.
package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
)

// TopicInfo holds metadata about a Kafka topic.
type TopicInfo struct {
	Name       string
	Partitions int32
}

// AdminClient provides Kafka admin operations needed by the operator.
type AdminClient interface {
	ListTopics(ctx context.Context) (map[string]TopicInfo, error)
	GetPartitionCount(ctx context.Context, topic string) (int32, error)
	Close()
}

// Config holds configuration for connecting to Kafka.
type Config struct {
	BootstrapServers []string
	Username         string
	Password         string
}

// ClientFactory creates an AdminClient from a Config.
type ClientFactory func(cfg Config) (AdminClient, error)

type adminClient struct {
	adm        *kadm.Client
	underlying *kgo.Client
}

// NewAdminClient creates a new Kafka admin client using franz-go.
func NewAdminClient(cfg Config) (AdminClient, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.BootstrapServers...),
		kgo.DialTimeout(5 * time.Second),
		kgo.RequestTimeoutOverhead(10 * time.Second),
	}

	if cfg.Username != "" {
		opts = append(opts, kgo.SASL(plain.Auth{
			User: cfg.Username,
			Pass: cfg.Password,
		}.AsMechanism()))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create kafka client: %w", err)
	}

	return &adminClient{
		adm:        kadm.NewClient(client),
		underlying: client,
	}, nil
}

func (c *adminClient) ListTopics(ctx context.Context) (map[string]TopicInfo, error) {
	topics, err := c.adm.ListTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}

	topics.FilterInternal()

	result := make(map[string]TopicInfo)
	for _, td := range topics.Sorted() {
		result[td.Topic] = TopicInfo{
			Name:       td.Topic,
			Partitions: int32(len(td.Partitions)),
		}
	}

	return result, nil
}

func (c *adminClient) GetPartitionCount(ctx context.Context, topic string) (int32, error) {
	topics, err := c.adm.ListTopics(ctx, topic)
	if err != nil {
		return 0, fmt.Errorf("describe topic %q: %w", topic, err)
	}

	td, ok := topics[topic]
	if !ok {
		return 0, fmt.Errorf("topic %q not found", topic)
	}

	return int32(len(td.Partitions)), nil
}

func (c *adminClient) Close() {
	c.underlying.Close()
}
