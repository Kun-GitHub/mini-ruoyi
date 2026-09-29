package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/domain"
)

// jobKey 是内置清理任务之一。key 里带冒号，顺便钉住
// 「带冒号的 key 能当路径参数用」——它直接来自代码注册表，不由界面决定。
const jobKey = "cleanup:orphan_files"

const jobLogsPath = "/api/v1/jobs/" + jobKey + "/logs"

type jobLogPage struct {
	List     []domain.JobLog `json:"list"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// jobLogs 取一次执行历史并解出 data。非 200 时只返回响应，交给调用方断言。
func jobLogs(t *testing.T, r *gin.Engine, path string, s sessionInfo) (jobLogPage, *httptest.ResponseRecorder) {
	t.Helper()

	w := call(t, r, http.MethodGet, path, "", s)
	var page jobLogPage
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(decode(t, w).Data, &page); err != nil {
			t.Fatalf("data 不是合法的分页结构: %v（%s）", err, w.Body.String())
		}
	}
	return page, w
}

// runAndWait 触发一次执行，等到历史里出现这条记录再返回它。
// 执行是异步的（RunNow 只表示「已触发」），所以必须轮询。
func runAndWait(t *testing.T, r *gin.Engine, admin sessionInfo) domain.JobLog {
	t.Helper()

	if w := call(t, r, http.MethodPost, "/api/v1/jobs/"+jobKey+"/run", "", admin); w.Code != http.StatusOK {
		t.Fatalf("触发任务失败: %d %s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if page, _ := jobLogs(t, r, jobLogsPath, admin); page.Total > 0 {
			return page.List[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("触发执行后 5 秒内没有出现执行记录")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestJobLogsRequireAuthAndPermission(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	// 未登录：401，而不是 403（与其它业务端点一致）
	if w := send(t, r, http.MethodGet, jobLogsPath, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录访问执行历史返回 %d，期望 401", w.Code)
	}

	// 登录了但没有任何权限：403
	userID := createUser(t, r, admin, "watcher", "watcher-pw")
	watcher := login(t, r, "watcher", "watcher-pw")
	if w := call(t, r, http.MethodGet, jobLogsPath, "", watcher); w.Code != http.StatusForbidden {
		t.Errorf("无权限访问执行历史返回 %d，期望 403", w.Code)
	}

	// 只授 tool:job:list —— 看历史属于「能看任务清单」，不该变成一个新的授权点。
	// 真变成新权限码的话，既有角色会突然看不到自己本来能看到的结果。
	w := call(t, r, http.MethodPost, "/api/v1/roles", `{"code":"watcher","name":"看任务"}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("建角色失败: %d %s", w.Code, w.Body.String())
	}
	var roleRes struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &roleRes); err != nil {
		t.Fatal(err)
	}
	if w := call(t, r, http.MethodPut, "/api/v1/roles/"+itoa(roleRes.Data.ID)+"/grants",
		`{"menu_ids":[],"perm_codes":["tool:job:list"]}`, admin); w.Code != http.StatusOK {
		t.Fatalf("授权失败: %d %s", w.Code, w.Body.String())
	}
	if w := call(t, r, http.MethodPut, "/api/v1/users/"+itoa(userID)+"/roles",
		`{"role_ids":[`+itoa(roleRes.Data.ID)+`]}`, admin); w.Code != http.StatusOK {
		t.Fatalf("绑角色失败: %d %s", w.Code, w.Body.String())
	}

	if _, w := jobLogs(t, r, jobLogsPath, watcher); w.Code != http.StatusOK {
		t.Errorf("有 tool:job:list 时访问执行历史返回 %d，期望 200（%s）", w.Code, w.Body.String())
	}
}

func TestJobLogsListRunHistory(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	// 全新库：还没有任何执行记录
	page, w := jobLogs(t, r, jobLogsPath, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("返回 %d: %s", w.Code, w.Body.String())
	}
	if page.Total != 0 || len(page.List) != 0 {
		t.Fatalf("全新库里就有 %d 条执行记录", page.Total)
	}
	if page.Page != 1 || page.PageSize != 20 {
		t.Errorf("分页回显 = {%d, %d}，期望 {1, 20}", page.Page, page.PageSize)
	}

	l := runAndWait(t, r, admin)
	if l.JobKey != jobKey {
		t.Errorf("job_key = %q，期望 %q", l.JobKey, jobKey)
	}
	if l.Trigger != domain.JobTriggerManual {
		t.Errorf("trigger = %q，期望 manual（是接口触发的，不是调度器）", l.Trigger)
	}
	if l.Status != domain.JobStatusSuccess {
		t.Errorf("status = %q，期望 success", l.Status)
	}
	if l.CreatedAt.IsZero() {
		t.Error("created_at 是零值——界面上的执行时间会显示成 0001 年")
	}
}

// TestJobLogsUnknownKeyIsNotFound 覆盖「代码里没有的任务」。
//
// 清单以代码注册表为准，与 GET /jobs 一致：库里残留的已删任务、
// 或者随手编的 key，都该是 404，而不是一个永远为空的列表。
func TestJobLogsUnknownKeyIsNotFound(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodGet, "/api/v1/jobs/no-such-job/logs", "", admin)
	if w.Code != http.StatusNotFound {
		t.Fatalf("返回 %d，期望 404（%s）", w.Code, w.Body.String())
	}
	if e := decode(t, w); e.Msg != "error.notFound" {
		t.Errorf("msg = %q，期望 error.notFound", e.Msg)
	}
}

// TestJobLogsPageIsClamped 覆盖过期书签：页码越界时钳到最后一页，
// 而不是返回空列表却回显一个不存在的页码——前端会显示「99 / 1」这种状态。
func TestJobLogsPageIsClamped(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	runAndWait(t, r, admin)

	page, w := jobLogs(t, r, jobLogsPath+"?page=99", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("返回 %d: %s", w.Code, w.Body.String())
	}
	if page.Page != 1 {
		t.Errorf("page=99 时回显的页码是 %d，期望被钳到最后一页 1", page.Page)
	}
	if len(page.List) == 0 {
		t.Error("钳到最后一页后应当还有数据可看")
	}
}
