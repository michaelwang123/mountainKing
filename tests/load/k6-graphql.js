/**
 * k6 负载测试 — mountainKing GraphQL API
 *
 * 使用方法:
 *   k6 run tests/load/k6-graphql.js
 *   k6 run --env BASE_URL=http://your-host:8080 tests/load/k6-graphql.js
 *   k6 run --env AUTH_TOKEN=your-jwt-token tests/load/k6-graphql.js
 *
 * 环境变量:
 *   BASE_URL   — GraphQL 端点基础 URL（默认 http://localhost:8080）
 *   AUTH_TOKEN — Bearer token（可选）
 */

import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";
const GRAPHQL_URL = `${BASE_URL}/graphql`;
const AUTH_TOKEN = __ENV.AUTH_TOKEN || "";

// ---------------------------------------------------------------------------
// Scenarios & Thresholds
// ---------------------------------------------------------------------------

export const options = {
  scenarios: {
    single_datasource: {
      executor: "constant-vus",
      vus: 10,
      duration: "30s",
      exec: "singleDatasource",
      tags: { scenario: "single_datasource" },
    },
    mixed_query: {
      executor: "constant-vus",
      vus: 20,
      duration: "30s",
      startTime: "35s", // start after single_datasource finishes
      exec: "mixedQuery",
      tags: { scenario: "mixed_query" },
    },
    template_query: {
      executor: "constant-vus",
      vus: 10,
      duration: "30s",
      startTime: "70s", // start after mixed_query finishes
      exec: "templateQuery",
      tags: { scenario: "template_query" },
    },
  },
  thresholds: {
    "http_req_duration{scenario:single_datasource}": [
      "p(95)<200",
      "p(99)<500",
    ],
    "http_req_duration{scenario:mixed_query}": ["p(95)<500", "p(99)<1000"],
  },
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function headers() {
  const h = { "Content-Type": "application/json" };
  if (AUTH_TOKEN) {
    h["Authorization"] = `Bearer ${AUTH_TOKEN}`;
  }
  return h;
}

function gql(query, variables) {
  const payload = JSON.stringify({ query, variables });
  const res = http.post(GRAPHQL_URL, payload, { headers: headers() });
  check(res, {
    "status is 200": (r) => r.status === 200,
    "no errors": (r) => {
      const body = r.json();
      return !body.errors || body.errors.length === 0;
    },
  });
  return res;
}

// ---------------------------------------------------------------------------
// Scenario: single_datasource — 单数据源查询
// ---------------------------------------------------------------------------

export function singleDatasource() {
  gql(
    `query {
      starrocks(
        datasource: "analytics_db"
        sql: "SELECT order_id, user_id, amount FROM orders LIMIT 10"
      ) {
        columns
        rows
        totalCount
      }
    }`
  );
  sleep(0.5);
}

// ---------------------------------------------------------------------------
// Scenario: mixed_query — 跨数据源混合查询
// ---------------------------------------------------------------------------

export function mixedQuery() {
  gql(
    `query {
      db: starrocks(
        datasource: "analytics_db"
        sql: "SELECT order_id, amount FROM orders LIMIT 5"
      ) {
        columns
        rows
      }
      prom: prometheus(
        datasource: "monitoring"
        query: "up"
      ) {
        columns
        rows
      }
    }`
  );
  sleep(0.5);
}

// ---------------------------------------------------------------------------
// Scenario: template_query — 模板查询
// ---------------------------------------------------------------------------

export function templateQuery() {
  gql(
    `query {
      templateQuery(
        name: "fleet_report"
        params: { eerid: "test-001", period: "monthly" }
      ) {
        columns
        rows
        totalCount
      }
    }`
  );
  sleep(0.5);
}
