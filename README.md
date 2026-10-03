# 保修维修配件工单工具

一个仅依赖 Go 标准库的本地命令行工具，把**保修资格判断 → 配件占用 → 维修完成**串成
可逐行查询、可重开保留数据的流程。

## 运行方式

```bash
go run ./cmd/warranty --state 状态目录
```

- 程序从标准输入**逐行读取 JSON 请求**，每读到一行就向标准输出写一行 JSON 响应。
- 非法 JSON 或业务校验失败的请求返回 `{"ok":false,"error":...}`，**不会中断进程**，继续处理下一行。
- 每次成功的写操作都会落盘到 `状态目录/warranty-state.json`（先写临时文件再原子重命名），
  正常退出后重开进程（再次 `go run` 指向同一目录）数据完整保留。

统一响应：

```json
{"ok": true, "result": { ... }}
{"ok": false, "error": "原因", "result": {"eligible": false, "reasons": ["..."]}}
```

## 请求格式（每行一个 JSON 对象）

| op | 必填字段 | 说明 |
| --- | --- | --- |
| `register_model` | `model_id`, `warranty_days`(正整数), `fault_categories`(非空、不重复) | 登记型号及保修规则 |
| `register_device` | `device_id`, `model_id`, `purchase_date` | 登记设备，登记时**固化**型号的保修天数与故障类别 |
| `receive_part` | `part_id`, `quantity`(正整数) | 配件入库，只增加可用库存 |
| `apply_repair` | `device_id`, `fault`, `apply_date` | 申请维修，生成 `pending` 工单，返回 `ticket_id` |
| `approve_ticket` | `ticket_id`，可选 `parts` | 批准待处理工单并占用配件；空清单表示无配件维修 |
| `cancel_ticket` | `ticket_id` | 取消待批准或已批准工单；已批准的释放全部占用配件 |
| `complete_ticket` | `ticket_id` | 完成已批准工单，占用配件转为消耗 |
| `query_ticket` | `ticket_id` | 查询工单、历史、资格及不符合原因 |
| `query_part` | `part_id` | 返回 `available` / `reserved` / `consumed` |
| `check_eligibility` | `device_id`, `fault`, `apply_date` | 只查资格与具体原因，不建工单 |

`parts` 元素形如 `{"part_id":"P1","quantity":2}`，数量必须为正整数，同一配件不能重复出现。

## 保修边界

- 所有日期为 `YYYY-MM-DD`。
- 保修区间从 `purchase_date` 当天起算，覆盖 `[购买日, 购买日 + 保修天数 - 1]`，共
  `warranty_days` 天。
- **到期日（购买日 + 保修天数）当天不保修**。例如购买日 `2026-01-01`、保修 30 天：
  `2026-01-30` 仍在保，`2026-01-31` 已过期。
- 申请日早于购买日、故障类别不在型号覆盖清单内，均不符合资格；`query_ticket` 与
  `check_eligibility` 会列出每一条具体原因。
- 设备登记后对型号规则做快照，型号本身不允许重复登记、也不影响既有设备。

## 工单与库存规则

- 工单编号全局唯一、递增（`T1`、`T2`……），状态流转：
  `pending` → `approved` → `completed`，或 `pending`/`approved` → `cancelled`。
- 同一设备同时只允许一个**未结束**工单（`pending`/`approved`）。
- 引用未知型号/设备/配件/工单、申请早于购买日等请求一律拒绝。
- 批准是**原子占用**：先校验所有配件都存在且库存充足，全部通过才一起扣减可用、增加占用；
  任何一个不足则整单失败，**不扣减任何库存**。
- 取消已批准工单：占用数量完整归还为可用库存，消耗数不变。
- 完成工单：占用数量转为累计消耗；之后再入库只增加可用数，**不会改写既有用量**。
- 工单 `history` 记录每次状态变化及当次配件去向（申请/批准/取消/完成）。
- 已结束（`cancelled`/`completed`）工单拒绝重复操作；`pending` 才能批准，`approved` 才能完成。

## 示例

输入：

```json
{"op":"register_model","model_id":"M1","warranty_days":30,"fault_categories":["screen","battery"]}
{"op":"register_device","device_id":"D1","model_id":"M1","purchase_date":"2026-01-01"}
{"op":"receive_part","part_id":"P1","quantity":5}
{"op":"apply_repair","device_id":"D1","fault":"screen","apply_date":"2026-01-30"}
{"op":"approve_ticket","ticket_id":"T1","parts":[{"part_id":"P1","quantity":2}]}
{"op":"complete_ticket","ticket_id":"T1"}
{"op":"query_part","part_id":"P1"}
```

最后一行输出：`available=3, reserved=0, consumed=2`。

库存不足示例（可用 5、申请 6）：

```json
{"op":"approve_ticket","ticket_id":"T1","parts":[{"part_id":"P1","quantity":6}]}
```

返回 `insufficient stock ...`，库存保持不变；改为取消工单后占用配件全部归还，
重启进程再 `query_part` 仍能看到归还后的数量。

## 测试

```bash
go test ./...
```

覆盖：保修边界（到期当天不保、早于购买日、类别不覆盖）、未知对象与参数校验、
库存不足时的原子不扣减、批准占用/完成消耗/取消释放、入库不改写累计消耗、
以及**跨进程重开后的状态保留与取消归还查询**。
