# RepairFlow Admin

RepairFlow 是售后维修运营后台，覆盖报修受理、故障诊断、报价确认、派工、上门记录、维修验收、费用登记和质保回访。调度员管理工单队列，主管复核异常和服务时效。

## 售后流程

1. 客户提交设备、故障现象和地址，生成 `待受理` 工单。
2. 技师补充诊断、配件和工时，运营人员确认报价并安排上门时段。
3. 服务人员在移动端记录到达、维修凭证和客户验收，工单进入 `已完成`。
4. 运营人员登记质保回访，回访结果和异常原因进入事件时间线。
5. 所有写请求要求 `Idempotency-Key`，Redis 负责幂等结果和并发锁，MySQL 8.4 负责持久化。

```bash
# 一键启动 API + MySQL 8.4 + Redis 8（会自动加载合成演示数据）
docker compose -f deploy/docker-compose.yml up --build

# 或仅使用无外部依赖的内存模式运行 API
go run ./server

# 管理后台
cd web && npm install && npm run dev
```

前端默认请求 `/api/v1`，Vite 开发服务器会把 `/api` 和 `/healthz` 代理到 `http://localhost:8080`。部署到独立域名时，可在构建时设置 `VITE_API_BASE_URL=https://api.example.com`；客户端会自动补齐 `/api/v1`，所有创建、确认、状态推进和回访完成请求都会自动生成 `Idempotency-Key`。

后台的“工单队列”“报价审批”“服务跟进”按钮会优先调用真实 API；API 暂不可用时保留内置演示数据并提示当前数据来源。侧栏“移动端体验”提供客户与技师的窄屏视图，支持报修、确认报价、更新到达状态、上传凭证和提交验收。

## API 示例

```bash
# 创建工单（重复发送相同 Idempotency-Key 只会创建一次）
curl -X POST http://localhost:8080/api/v1/appointments \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: demo-create-001' \
  -d '{"patient":"演示客户","department":"全科门诊","doctor":"林负责人","scheduledAt":"2026-07-16T09:00:00+08:00"}'

# 推进状态：待确认 -> 已确认 -> 候诊中 -> 处理中 -> 已完成（将 AP-1001 替换为上一步返回的 id）
curl -X POST http://localhost:8080/api/v1/appointments/AP-1001/checkin -H 'Idempotency-Key: demo-checkin-001'
curl -X POST http://localhost:8080/api/v1/appointments/AP-1001/status \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: demo-waiting-001' -d '{"status":"候诊中"}'

# 查看审计事件
curl http://localhost:8080/api/v1/appointments/AP-1001/events

# 完成回访
curl -X POST http://localhost:8080/api/v1/followups/FW-0716-001/complete -H 'Idempotency-Key: demo-followup-001'
```

演示数据均为虚构数据；项目不得用于真实医疗诊断、处方、支付或客户隐私存储。

## 运行范围

RepairFlow 覆盖报修、诊断、报价、派工、维修、验收和质保回访的售后操作；所有演示数据均为虚构，不接入真实财务、人事或客户隐私。

