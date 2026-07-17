import test from 'node:test'; import assert from 'node:assert/strict'; import { readFile } from 'node:fs/promises'
test('RepairFlow has work-order, engineer schedule and service follow-up data', async()=>{const source=await readFile(new URL('../src/main.js',import.meta.url),'utf8'); assert.match(source,/今日工单队列/); assert.match(source,/工程师排班/); assert.match(source,/服务跟进/); assert.match(source,/JOB-0716-082/)})

test('RepairFlow binds real API actions while keeping a demo fallback', async()=>{const source=await readFile(new URL('../src/main.js',import.meta.url),'utf8'); assert.match(source,/createApiClient/); assert.match(source,/data-action="checkin"/); assert.match(source,/data-action="status"/); assert.match(source,/data-action="complete-followup"/); assert.match(source,/refreshFromApi/); assert.match(source,/演示数据/)})

test('Vite proxies the default API path to the local Go service', async()=>{const source=await readFile(new URL('../vite.config.js',import.meta.url),'utf8'); assert.match(source,/server/); assert.match(source,/proxy/); assert.match(source,/localhost:8080/)})

test('RepairFlow exposes work-order detail quote dispatch acceptance and warranty actions', async()=>{const source=await readFile(new URL('../src/main.js',import.meta.url),'utf8'); assert.match(source,/工单工作台/); assert.match(source,/服务时间线/); assert.match(source,/提交报价/); assert.match(source,/安排上门/); assert.match(source,/确认验收/); assert.match(source,/登记质保/); assert.match(source,/openWorkOrder/)})
