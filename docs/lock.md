# sync.Mutex

自旋锁: 使用 CAS (Compare And Swap)

锁策略

- 主动轮询: CAS + 自旋, 锁竞争者持续请求获取锁, 即使锁已被占用, 直到获取锁成功
- 信号量 park/unpark: 阻塞 + 唤醒, 锁竞争者发现锁已被占用时, 创建 listener 监听器, 定于锁释放事件; 之后不再持续请求获取锁; 直到锁释放事件发布, 再重新请求获取锁

| 锁竞争                          | 特点                           | 适用场景   |
| ------------------------------- | ------------------------------ | ---------- |
| 主动轮询: CAS + 自旋            | 不阻塞协程, 没有上下文切换开销 | 并发不激烈 |
| 信号量 park/unpark: 阻塞 + 唤醒 | 阻塞协程, 有上下文切换开销     | 并发激烈   |

## sync.Mutex 锁升级

1. 升级前: 乐观锁, goroutine 使用主动轮询: CAS + 自旋
2. 升级后: 悲观锁, goroutine 使用信号量 park/unpark: 阻塞 + 唤醒

乐观锁升级到悲观锁的条件: 自旋 4 次仍然获取锁失败

跳过自旋的条件

```go
func runtimeCanSpin() {
  return GOMAXPROCS > 1 && len(Process 的本地 goroutine 队列) === 0 && 自旋次数 < 4
}
```

1. 单核 CPU 或者 GOMAXPROCS = 1

- G2 占有锁, 在 Processor 上运行
- G1 获取锁失败, 自旋...
- G1 CAS 循环, 跑满 Processor
- G2 无法执行 Unlock() 释放锁
- G1 CAS 死循环

2. Processor 的本地 goroutine 队列中有就绪、等待运行的 Goroutine

## GMP

GMP

- Goroutine 协程 (工人)
- Machine 线程 (工单)
- Processor 处理器 (工位)
  GOMAXPROCS: Processor 处理器数量, 默认 GOMAXPROCS == CPU 数量 `runtime.NumCPU()`

每个 Processor 维护一个就绪本地 goroutine 队列 (runq), 是一个容量 256 goroutines 的环形缓冲区

```txt
  全局 goroutine 队列 (global runq)
    │
┌────Processor ──────────────────────┐
│  本地 goroutine 队列 (local runq)  │
│  [G0][G1][G2]...[G255]             │
└────────────────────────────────────┘
    │
  Machine (线程) 取出 Goroutine 执行
```

- 入队: 新创建的 Goroutine, 阻塞被唤醒的 Goroutine, 优先加入 Processor 的本地 goroutine 队列
- 出队: 绑定到该 Processor 的 Machine 从本地 goroutine 队列头取出 Goroutine 执行
- 本地 goroutine 队列满: 本地 goroutine 队列满 256 个时, 转移 1/2 的 goroutines 到全局队列
- 本地 goroutine 队列空: Processor 先尝试从全局 goroutine 队列中取「一批」goroutine, 再尝试从其他 Processor 中偷「一半」的 goroutine (work stealing)

Goroutine: 异步 task
Machine: 异步 task 的执行者
Processor: 异步 task 队列

数量: Goroutine >> Machine ≈ Processor

为什么需要 Processor

- 如果没有 Processor (的本地 goroutine 队列), 则所有的 Machine 都去全局队列取任务 -> 需要加锁
- 有 Processor (的本地 goroutine 队列):
  - 任意时刻, 一个 Processor 的本地 goroutine 队列只有一个 Machine 在消费 -> 不需要加锁
  - 只有当本地 goroutine 队列空时
    - Processor 先尝试从全局 goroutine 队列中取「一批」goroutine -> 加互斥锁
    - Processor 再尝试从其他 Processor 中偷「一半」的 goroutine (work stealing) -> CAS + 自旋
