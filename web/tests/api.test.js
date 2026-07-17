import test from 'node:test'
import assert from 'node:assert/strict'

import { createApiClient } from '../src/api.js'

function response(data, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    async json() {
      return { code: 0, message: 'ok', data }
    },
  }
}

test('defaults to /api/v1 and adds an idempotency key to writes', async () => {
  const requests = []
  const client = createApiClient({
    fetchImpl: async (url, init) => {
      requests.push({ url, init })
      return response({ id: 'AP-1', status: '已确认' })
    },
  })

  const appointment = await client.checkinAppointment('AP-1')

  assert.equal(appointment.id, 'AP-1')
  assert.equal(requests[0].url, '/api/v1/appointments/AP-1/checkin')
  assert.equal(requests[0].init.method, 'POST')
  assert.match(requests[0].init.headers['Idempotency-Key'], /^cf-/)
})

test('uses a configured API origin without duplicating the API path', async () => {
  const requests = []
  const client = createApiClient({
    baseUrl: 'http://localhost:8080/api/v1/',
    fetchImpl: async (url) => {
      requests.push(url)
      return response({ list: [], total: 0 })
    },
  })

  await client.listAppointments({ page: 1, pageSize: 20 })

  assert.equal(requests[0], 'http://localhost:8080/api/v1/appointments?page=1&pageSize=20')
})

test('rejects non-zero API envelopes so callers can keep demo data', async () => {
  const client = createApiClient({
    fetchImpl: async () => ({
      ok: false,
      status: 409,
      async json() {
        return { code: 409, message: '状态不可推进', data: null }
      },
    }),
  })

  await assert.rejects(() => client.updateAppointmentStatus('AP-1', '候诊中'), /状态不可推进/)
})

test('exposes mobile lifecycle and follow-up operations through the same client', async () => {
  const paths = []
  const client = createApiClient({
    fetchImpl: async (url) => {
      paths.push(url)
      return response({ id: 'ok' })
    },
  })

  await client.createAppointment({ patient: '演示客户', department: '全科门诊' })
  await client.checkinAppointment('AP-1')
  await client.updateAppointmentStatus('AP-1', '候诊中')
  await client.updateAppointmentStatus('AP-1', '处理中')
  await client.updateAppointmentStatus('AP-1', '已完成')
  await client.completeFollowup('FW-1')

  assert.deepEqual(paths, [
    '/api/v1/appointments',
    '/api/v1/appointments/AP-1/checkin',
    '/api/v1/appointments/AP-1/status',
    '/api/v1/appointments/AP-1/status',
    '/api/v1/appointments/AP-1/status',
    '/api/v1/followups/FW-1/complete',
  ])
})

test('exposes repair work-order quote dispatch acceptance and warranty lifecycle', async () => {
  const paths = []
  const client = createApiClient({ fetchImpl: async (url) => { paths.push(url); return response({ id: 'WO-1', status: '已诊断' }) } })
  await client.listWorkOrders({ page: 1, pageSize: 20, status: '待报价' })
  await client.getWorkOrder('WO-1')
  await client.createWorkOrder({ customerName: '林先生', description: '空调故障' })
  await client.updateWorkOrderStatus('WO-1', '已诊断')
  await client.quoteWorkOrder('WO-1', { laborCents: 8000, materialCents: 12000 })
  await client.dispatchWorkOrder('WO-1', { technician: '周师傅', scheduledAt: '2026-07-18T14:00:00+08:00' })
  await client.acceptWorkOrder('WO-1', { result: '维修完成', customerSign: '林先生' })
  await client.createWorkOrderWarranty('WO-1', { expiresAt: '2026-08-18' })
  assert.deepEqual(paths, [
    '/api/v1/work-orders?page=1&pageSize=20&status=%E5%BE%85%E6%8A%A5%E4%BB%B7',
    '/api/v1/work-orders/WO-1',
    '/api/v1/work-orders',
    '/api/v1/work-orders/WO-1/status',
    '/api/v1/work-orders/WO-1/quote',
    '/api/v1/work-orders/WO-1/dispatch',
    '/api/v1/work-orders/WO-1/acceptance',
    '/api/v1/work-orders/WO-1/warranty',
  ])
})
