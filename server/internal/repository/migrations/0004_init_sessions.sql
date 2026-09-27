-- 0004 会话表
--
-- 认证方案是 Cookie + 服务端 session（见 docs/architecture.md §5 的决策记录），
-- 因此需要一个服务端表来存会话状态，这样「登出 / 踢人 / 禁用用户」才能立即生效。
--
-- token 存的是 SHA-256 而不是明文：数据库文件一旦泄露（本项目的 data.db 随仓库分发，
-- 用户也可能随手备份），明文 token 等于把所有人的会话直接送出去，而哈希值无法反推。

CREATE TABLE sys_sessions (
    -- token 的 SHA-256，base64url 编码
    token_hash   varchar(64) PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES sys_users(id) ON DELETE CASCADE,
    created_at   datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   datetime NOT NULL,
    last_seen_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- CSRF 令牌随会话下发，前端放在请求头里回传。
    -- 不用 HMAC 派生是为了避免再引入一个服务端密钥配置。
    csrf_token   varchar(64) NOT NULL,
    ip           varchar(64)  NOT NULL DEFAULT '',
    user_agent   varchar(255) NOT NULL DEFAULT ''
);

-- 「踢掉某用户的全部会话」「清理过期会话」都要走这两列
CREATE INDEX idx_sessions_user    ON sys_sessions(user_id);
CREATE INDEX idx_sessions_expires ON sys_sessions(expires_at);
