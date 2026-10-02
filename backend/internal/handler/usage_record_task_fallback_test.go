package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 计费任务在 worker 池停止或队列满时必须内联同步执行，不能被 drop/sample 丢弃。

func newStoppedUsageRecordPoolForTest() *service.UsageRecordWorkerPool {
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount:    1,
		QueueSize:      1,
		TaskTimeout:    time.Second,
		OverflowPolicy: config.UsageRecordOverflowPolicySync,
	})
	pool.Stop()
	return pool
}

func TestGatewayHandlerSubmitUsageRecordTask_StoppedPoolFallsBackToSync(t *testing.T) {
	h := &GatewayHandler{usageRecordWorkerPool: newStoppedUsageRecordPoolForTest()}

	executed := false
	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		executed = true
	})
	require.True(t, executed, "池已停止时计费任务必须内联同步执行")
}

func TestOpenAIGatewayHandlerSubmitUsageRecordTask_StoppedPoolFallsBackToSync(t *testing.T) {
	h := &OpenAIGatewayHandler{usageRecordWorkerPool: newStoppedUsageRecordPoolForTest()}

	executed := false
	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		executed = true
	})
	require.True(t, executed, "池已停止时计费任务必须内联同步执行")
}

func TestUsageRecordTask_OverflowAlwaysBillsExactlyOnce(t *testing.T) {
	for _, policy := range []string{
		config.UsageRecordOverflowPolicyDrop,
		config.UsageRecordOverflowPolicySample,
		config.UsageRecordOverflowPolicySync,
	} {
		for _, handler := range []string{"gateway", "openai", "openai_text_result", "openai_nil_result"} {
			t.Run(policy+"/"+handler, func(t *testing.T) {
				pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
					WorkerCount:           1,
					QueueSize:             1,
					TaskTimeout:           time.Second,
					OverflowPolicy:        policy,
					OverflowSamplePercent: 10,
				})
				t.Cleanup(pool.Stop)

				started, release := make(chan struct{}), make(chan struct{})
				released := false
				t.Cleanup(func() {
					if !released {
						close(release)
					}
				})
				// Keep the worker and its queue occupied throughout the billing submissions.
				require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(context.Context) {
					close(started)
					<-release
				}))
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("worker did not start")
				}
				require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(context.Context) {}))

				gateway := &GatewayHandler{usageRecordWorkerPool: pool}
				openai := &OpenAIGatewayHandler{usageRecordWorkerPool: pool}
				// Disconnecting a client must not cancel the billing fallback or lose request attribution.
				parent, cancel := context.WithCancel(context.WithValue(context.Background(), ctxkey.RequestID, "billing-request"))
				cancel()
				var executions atomic.Int64
				// A full sample cycle verifies both sampled and non-sampled submissions.
				for i := int64(1); i <= 100; i++ {
					task := func(ctx context.Context) {
						require.NoError(t, ctx.Err())
						_, hasDeadline := ctx.Deadline()
						require.True(t, hasDeadline, "billing fallback must have a timeout")
						require.Equal(t, "billing-request", ctx.Value(ctxkey.RequestID))
						executions.Add(1)
					}
					switch handler {
					case "gateway":
						gateway.submitUsageRecordTask(parent, task)
					case "openai":
						openai.submitUsageRecordTask(parent, task)
					case "openai_text_result":
						openai.submitOpenAIUsageRecordTask(parent, &service.OpenAIForwardResult{}, task)
					case "openai_nil_result":
						openai.submitOpenAIUsageRecordTask(parent, nil, task)
					}
					require.Equal(t, i, executions.Load(), "billing must finish inline before the submission returns")
				}
				close(release)
				released = true
				pool.Stop()
				require.EqualValues(t, 100, executions.Load(), "draining the queue must not bill a request twice")
			})
		}
	}
}
