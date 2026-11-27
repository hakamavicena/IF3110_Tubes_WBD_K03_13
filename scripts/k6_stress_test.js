import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

const apiErrorRate = new Rate("platform_api_error_rate");
const jobCompletionTime = new Trend("platform_job_completion_ms", true);
const queueWaitTime = new Trend("platform_queue_wait_ms", true);
const jobsSucceeded = new Counter("platform_jobs_succeeded_total");

export const options = {
  stages: [
    { duration: "15s", target: 5 },   // Level 1: Beban Normal (Warm-up)
    { duration: "30s", target: 15 },  // Level 2: Beban Menengah (Above Normal)
    { duration: "30s", target: 30 },  // Level 3: Beban Puncak (Stress Peak)
    { duration: "15s", target: 5 },   // Level 4: Penurunan Beban (Recovery)
    { duration: "10s", target: 0 },   // Cool-down
  ],
  thresholds: {
    http_req_duration: ["p(50)<250", "p(95)<800"],
    http_req_failed: ["rate<0.05"],
    platform_api_error_rate: ["rate<0.05"],
    platform_job_completion_ms: ["p(95)<15000"],
  },
};

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";
const USERNAME = __ENV.ADMIN_USERNAME || "admin";
const PASSWORD = __ENV.ADMIN_PASSWORD || "admin12345";
const DB_ID_PRIMARY = Number(__ENV.DB_ID_1 || 1);
const DB_ID_SECONDARY = Number(__ENV.DB_ID_2 || 2);

function loginAndGetSession() {
  const res = http.post(
    `${BASE_URL}/api/auth/login`,
    JSON.stringify({ username: USERNAME, password: PASSWORD }),
    { headers: { "Content-Type": "application/json" } }
  );
  const ok = check(res, {
    "login status 200": (r) => r.status === 200,
  });
  apiErrorRate.add(!ok);
  if (!ok) {
    return null;
  }
  const body = res.json();
  return {
    csrfToken: body && body.csrf_token ? String(body.csrf_token) : "",
  };
}

export default function () {
  const session = loginAndGetSession();
  if (!session) {
    sleep(1);
    return;
  }

  const authHeaders = {
    "Content-Type": "application/json",
    "X-CSRF-Token": session.csrfToken,
  };

  // 1. Uji akses API Read-Only (Daftar Database, Schema, & History)
  const listDbRes = http.get(`${BASE_URL}/api/databases`, {
    headers: authHeaders,
  });
  apiErrorRate.add(listDbRes.status !== 200);
  check(listDbRes, {
    "GET /api/databases 200": (r) => r.status === 200,
  });

  // Pilih database target secara bergantian (menguji pengiriman job paralel
  // pada database yang SAMA maupun database yang BERBEDA)
  const targetDbId = __VU % 2 === 0 ? DB_ID_PRIMARY : DB_ID_SECONDARY;

  const schemasRes = http.get(
    `${BASE_URL}/api/databases/${targetDbId}/schemas`,
    { headers: authHeaders }
  );
  if (schemasRes.status !== 200 && schemasRes.status !== 404) {
    apiErrorRate.add(true);
  } else {
    apiErrorRate.add(false);
  }

  const historyRes = http.get(
    `${BASE_URL}/api/databases/${targetDbId}/history?hide_rollback=false`,
    { headers: authHeaders }
  );
  check(historyRes, {
    "GET /history 200 or 404": (r) => r.status === 200 || r.status === 404,
  });

  // 2. Uji pengiriman job migration secara bersamaan & pantau hingga selesai
  // (Respons pengiriman job HTTP 202 tidak dihitung sebagai keberhasilan eksekusi;
  //  script melakukan polling status job hingga mencapai status 'success')
  if (__ITER % 3 === 0) {
    const createRes = http.post(
      `${BASE_URL}/api/databases/${targetDbId}/migrations`,
      JSON.stringify({
        title: `k6 stress migration vu-${__VU}-iter-${__ITER}`,
        sql: "SELECT 1;",
        sql_down: "SELECT 1;",
        is_irreversible: false,
        execution_mode: "standard",
      }),
      { headers: authHeaders }
    );

    if (createRes.status === 201) {
      const created = createRes.json();
      const jobId = created && created.job ? created.job.id : 0;

      if (jobId > 0) {
        http.post(
          `${BASE_URL}/api/databases/${targetDbId}/jobs/${jobId}/submit-review`,
          null,
          { headers: authHeaders }
        );
        const approveRes = http.post(
          `${BASE_URL}/api/databases/${targetDbId}/jobs/${jobId}/approve`,
          null,
          { headers: authHeaders }
        );

        if (approveRes.status === 200) {
          const enqueueStart = Date.now();
          const enqRes = http.post(
            `${BASE_URL}/api/databases/${targetDbId}/jobs/${jobId}/enqueue`,
            null,
            { headers: authHeaders }
          );

          check(enqRes, {
            "POST /enqueue returns 202 Accepted": (r) => r.status === 202,
          });

          if (enqRes.status === 202) {
            let finished = false;
            for (let poll = 0; poll < 30; poll++) {
              sleep(0.25);
              const pollRes = http.get(
                `${BASE_URL}/api/databases/${targetDbId}/jobs/${jobId}`,
                { headers: authHeaders }
              );
              if (pollRes.status === 200) {
                const jobData = pollRes.json().job;
                if (
                  jobData &&
                  (jobData.status === "success" ||
                    jobData.status === "failed" ||
                    jobData.status === "partial")
                ) {
                  finished = true;
                  const totalElapsed = Date.now() - enqueueStart;
                  jobCompletionTime.add(totalElapsed);
                  if (typeof jobData.duration_ms === "number") {
                    const qWait = Math.max(0, totalElapsed - jobData.duration_ms);
                    queueWaitTime.add(qWait);
                  }
                  if (jobData.status === "success") {
                    jobsSucceeded.add(1);
                  }
                  break;
                }
              }
            }
            check(finished, {
              "job reached terminal execution status": (done) => done === true,
            });
          }
        }
      }
    }
  }

  sleep(0.5);
}
