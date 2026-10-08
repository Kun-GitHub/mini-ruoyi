package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"mini-ruoyi/internal/csvx"
	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/middleware"
	"mini-ruoyi/internal/repository"
	"mini-ruoyi/internal/service"
)

type UserHandler struct {
	svc *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// 校验规则的取值必须与数据库的 CHECK 约束一致（见 docs/schema.md）。
// 标签里的规则只能写字面量——Go 的 struct tag 不接受常量拼接，
// 所以改了 CHECK 约束就要同时搜一遍 oneof= 来对齐。
type createUserRequest struct {
	Username string `json:"username" binding:"required,min=2,max=128"`
	// max=72 是 bcrypt 的上限，见 auth.MaxPasswordBytes
	Password string `json:"password" binding:"required,min=8,max=72"`
	Nickname string `json:"nickname" binding:"max=128"`
	Mobile   string `json:"mobile" binding:"max=20"`
	Email    string `json:"email" binding:"omitempty,email,max=64"`
	Status   string `json:"status" binding:"omitempty,oneof=active inactive"`
}

func (h *UserHandler) Create(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	u, err := h.svc.Create(c.Request.Context(), service.CreateUserInput{
		Username: req.Username,
		Password: req.Password,
		Nickname: req.Nickname,
		Mobile:   req.Mobile,
		Email:    req.Email,
		Status:   req.Status,
	})
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, u)
}

func (h *UserHandler) List(c *gin.Context) {
	page, pageSize := pageParams(c)
	filter, err := userFilter(c)
	if err != nil {
		return
	}
	result, err := h.svc.List(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

// userExportHeader 是导出的列。
//
// **不含 password**：哈希导出去没有任何用处，而一份会被转发、会被存盘的
// 文件里出现凭据字段本身就是风险。列名与导入按名匹配，所以这份文件
// 可以直接导回去（多余的 login_* 列会被忽略）。
var userExportHeader = []string{
	"username", "nickname", "mobile", "email", "status", "login_ip", "login_at", "created_at",
}

// Export 把用户列表按当前筛选条件导出成 CSV。
func (h *UserHandler) Export(c *gin.Context) {
	filter, err := userFilter(c)
	if err != nil {
		return
	}
	svc := h.svc
	streamCSV(c, csvFilename("users", time.Now()), userExportHeader,
		func(ctx context.Context, page, size int) (service.Page[domain.User], error) {
			return svc.List(ctx, filter, page, size)
		},
		func(u domain.User) []string {
			return []string{
				u.Username, u.Nickname, u.Mobile, u.Email, u.Status,
				u.LoginIP, csvTimePtr(u.LoginAt), csvTime(u.CreatedAt),
			}
		})
}

func (h *UserHandler) Get(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	detail, err := h.svc.GetDetail(c.Request.Context(), id)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, detail)
}

// Username 有意不在可改字段里：用户名是身份标识，改了会让审计记录失去意义，
// 所以数据库层面它也叫「不可变」。
type updateUserRequest struct {
	Nickname string `json:"nickname" binding:"max=128"`
	Mobile   string `json:"mobile" binding:"max=20"`
	Email    string `json:"email" binding:"omitempty,email,max=64"`
	Status   string `json:"status" binding:"required,oneof=active inactive"`
}

func (h *UserHandler) Update(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if err := h.svc.Update(c.Request.Context(), domain.User{
		ID:       id,
		Status:   req.Status,
		Nickname: req.Nickname,
		Mobile:   req.Mobile,
		Email:    req.Email,
	}); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

func (h *UserHandler) Delete(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	// 当前登录用户从上下文取，不由客户端传入——
	// 否则只要伪造一个 id 就能绕过「不能删自己」的守卫
	currentUserID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id, currentUserID); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

type setRolesRequest struct {
	RoleIDs []int64 `json:"role_ids" binding:"required"`
}

func (h *UserHandler) SetRoles(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	var req setRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if err := h.svc.SetRoles(c.Request.Context(), id, req.RoleIDs); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id, "role_ids": req.RoleIDs})
}

type resetPasswordRequest struct {
	Password string `json:"password" binding:"required,min=8,max=72"`
}

func (h *UserHandler) ResetPassword(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), id, req.Password); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

// ---- 用户导入 ----

const (
	// userImportMaxRows 是单次导入的数据行上限。
	//
	// 每行都要过一次库，1C1G 的机器经不起一个手滑的几十万行文件；
	// 500 行也是一份「人肉整理出来的名单」的合理规模。
	userImportMaxRows = 500
)

// userImportRequired 是导入文件必须有的列，按**列名**匹配（与顺序无关）。
//
// 只要求 username：其余列在库里都有默认值，要求全套会把「只想批量建号」
// 的人挡在外面。多余列一律忽略，所以导出的文件可以原样导回来。
var userImportRequired = []string{"username"}

// importUsersRequest 是导入请求里除文件之外的字段。
//
// 密码**不进 CSV**：明文凭据不该落在一份会被转发、会被留档的文件里。
// 它在界面表单里单独传，整批共用一个——服务端因此只需要哈希一次。
type importUsersRequest struct {
	// max=72 是 bcrypt 的上限，与 createUserRequest / resetPasswordRequest 一致
	Password string `form:"password" json:"password" binding:"required,min=8,max=72"`
}

// importReport 是导入接口的 data 形状。
//
// 逐行错误随 200 的 data 返回，而不是装进 4xx 的 errors：前端 client.ts 的
// ApiError 只保留 code/msg/errors，行号这类细节装在失败响应里会被丢弃，
// 而「哪一行错了」正是导入结果里最有用的信息。走 4xx 的只有「连文件都读不了」
// 的情况：没带文件、超行数、缺列、CSV 本身是坏的。
type importReport struct {
	Total    int                      `json:"total"`
	Valid    int                      `json:"valid"`
	Imported int                      `json:"imported"`
	Errors   []service.ImportRowError `json:"errors"`
}

// Import 从 CSV 批量建用户。
//
// ?dry_run=1 只校验不写库，报告与真导入同形——界面上的「校验」按钮走它。
func (h *UserHandler) Import(c *gin.Context) {
	dryRun := c.Query("dry_run") == "1"

	file, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyImportNoFile)
		return
	}
	src, err := file.Open()
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyImportNoFile)
		return
	}
	defer src.Close() //nolint:errcheck // 只读文件，关闭失败不影响结果

	var req importUsersRequest
	if err := c.ShouldBind(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}

	header, rows, err := csvx.Read(src, userImportMaxRows)
	switch {
	case errors.Is(err, csvx.ErrTooManyRows):
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyImportTooManyRows)
		return
	case err != nil:
		// 含空文件，以及「某一行多了一个没转义的逗号」导致的列数不一致。
		// 后者继续解析只会把数据错位写进库，所以宁可整份拒绝。
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyMalformedBody)
		return
	}

	cols := make(map[string]int, len(header))
	for i, name := range header {
		cols[strings.TrimSpace(name)] = i
	}
	missing := make([]string, 0, len(userImportRequired))
	for _, name := range userImportRequired {
		if _, ok := cols[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		httpx.FailData(c, http.StatusBadRequest, httpx.KeyImportMissingColumns,
			gin.H{"missing": missing})
		return
	}

	importRows := make([]service.ImportRow, 0, len(rows))
	rowErrs := make([]service.ImportRowError, 0)
	seen := make(map[string]int, len(rows))

	for i, rec := range rows {
		// 行号按**文件**算：表头占第 1 行，用户能拿着它回到文件里定位
		line := i + service.ImportFirstDataRow
		row := service.ImportRow{
			Username: cell(rec, cols, "username"),
			Nickname: cell(rec, cols, "nickname"),
			Mobile:   cell(rec, cols, "mobile"),
			Email:    cell(rec, cols, "email"),
			Status:   cell(rec, cols, "status"),
		}
		importRows = append(importRows, row)

		// 校验规则挂在 service.ImportRow 上，与手工建号的规则同源；
		// 用 gin 的校验器是为了让字段名按 json 标签输出（username 而不是 Username）
		if err := binding.Validator.ValidateStruct(&row); err != nil {
			for _, fe := range httpx.FieldErrors(err) {
				rowErrs = append(rowErrs, service.ImportRowError{
					Row: line, Field: fe.Field, Rule: fe.Rule, Param: fe.Param,
				})
			}
		}

		// 文件内部的自撞先于数据库判断：同一批里出现两次的 username 是文件问题，
		// 报的必须是「跟文件第几行撞了」，用户才回得去改。
		// 空用户名已经被 required 拦下，不参与重复判断，免得再叠一条噪声。
		if row.Username != "" {
			if first, dup := seen[row.Username]; dup {
				rowErrs = append(rowErrs, service.ImportRowError{
					Row: line, Field: "username", Rule: "duplicateRow", Param: strconv.Itoa(first),
				})
			} else {
				seen[row.Username] = line
			}
		}
	}

	// 有任何行级错误就把 dryRun 传成 true：一行不合格就一行都不写，
	// 这个「不写」的决定在这一层就已经做出来了（数据库侧的唯一冲突由服务层判定）。
	res, err := h.svc.Import(c.Request.Context(), importRows, req.Password, dryRun || len(rowErrs) > 0)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}

	errs := append(rowErrs, res.Errors...)
	sort.Slice(errs, func(i, j int) bool { return errs[i].Row < errs[j].Row })
	if errs == nil {
		// 序列化成 [] 而不是 null：前端读 report.errors.length，null 会直接抛错
		errs = []service.ImportRowError{}
	}
	httpx.Success(c, importReport{
		Total:    len(rows),
		Valid:    len(rows) - rowsWithErrors(errs),
		Imported: res.Imported,
		Errors:   errs,
	})
}

// rowsWithErrors 数出有错误的**行数**。
//
// 一行可能同时踩两条规则，而报告里的 valid 是「能进库的行数」，按行去重。
func rowsWithErrors(errs []service.ImportRowError) int {
	rows := make(map[int]struct{}, len(errs))
	for _, e := range errs {
		rows[e.Row] = struct{}{}
	}
	return len(rows)
}

// ---- 本包内共用的解析辅助 ----

// pathID 解析路径参数 id。失败时已经写好 400 响应，调用方直接 return 即可。
func pathID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidID)
		return 0, err
	}
	return id, nil
}

func pageParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	return page, pageSize
}

// userFilter 解析用户列表与导出共用的筛选条件。
//
// 两边各解析一次的话，「屏幕上的列表」和「导出的文件」迟早会对不上，
// 而这类不一致最难发现——文件看起来总是对的。
// 失败时已经写好 400 响应，调用方直接 return 即可。
func userFilter(c *gin.Context) (repository.UserFilter, error) {
	roleID, err := optionalID(c, "role_id")
	if err != nil {
		return repository.UserFilter{}, err
	}
	return repository.UserFilter{
		Username: c.Query("username"),
		Nickname: c.Query("nickname"),
		Mobile:   c.Query("mobile"),
		Status:   c.Query("status"),
		RoleID:   roleID,
	}, nil
}

// cell 按列名取一行里的单元格。
//
// 只有 userImportRequired 里的列被保证存在，其余列都可以缺，缺了就是空串。
// 这里不查下标越界：csvx.Read 用 encoding/csv 的默认 FieldsPerRecord，
// 每行列数必须与表头一致，否则整份文件会被拒。
func cell(rec []string, cols map[string]int, name string) string {
	i, ok := cols[name]
	if !ok {
		return ""
	}
	return rec[i]
}

// optionalID 解析一个可选的查询参数 ID。空串返回 0，表示不筛。
//
// 非法时报 400 而不是当作「不筛」——静默忽略的话筛选看起来生效了，
// 实际拿到的是全部数据，用户会对着没筛过的结果找问题。
// 失败时已经写好 400 响应，调用方直接 return 即可。
func optionalID(c *gin.Context, name string) (int64, error) {
	raw := c.Query(name)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidID)
		return 0, fmt.Errorf("非法的 %s=%q", name, raw)
	}
	return id, nil
}

// dateRange 解析日志筛选的 start / end 两个日期参数（YYYY-MM-DD）。
//
// 按**服务器本地时区**解释，与 cron 保持一致——「今天」是运维看到的那个今天。
// 代价是服务器与浏览器不在同一时区时会有一天以内的偏移，界面上显示的也是本地时间，
// 所以自托管场景下两者一致。
//
// 返回的 End 是**开区间上界**：界面上选的是「日期」而不是「时刻」，
// 选 10-01 到 10-01 应当包含那一整天，所以这里 +1 天。
//
// 格式错时已经写好 400 响应，调用方直接 return 即可。静默忽略是不行的——
// 用户会以为筛选生效了，对着一份没筛过的数据找问题。
func dateRange(c *gin.Context) (time.Time, time.Time, error) {
	start, err := parseDate(c.Query("start"))
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidDate)
		return time.Time{}, time.Time{}, err
	}
	end, err := parseDate(c.Query("end"))
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidDate)
		return time.Time{}, time.Time{}, err
	}
	if !end.IsZero() {
		end = end.AddDate(0, 0, 1)
	}
	return start, end, nil
}

// parseDate 把 YYYY-MM-DD 解析成服务器本地时区的零点；空串返回零值。
func parseDate(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	d, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("解析日期 %q: %w", raw, err)
	}
	return d, nil
}
