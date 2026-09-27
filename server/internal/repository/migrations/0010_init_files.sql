-- 0010 文件管理
--
-- 元数据进库，**文件本体落磁盘**（APP_UPLOAD_DIR）。
-- 不能存 BLOB：data.db 是随仓库分发的，BLOB 会让仓库无限膨胀，
-- 而且 git 也没法管理二进制。
--
-- storage_path 是磁盘上的相对路径（由 UUID 组成），绝不使用用户给的文件名——
-- 直接拿用户输入当路径就是路径穿越（../../etc/passwd）。
-- original_name 只用于展示与下载时的文件名。

CREATE TABLE sys_files (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at    datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- 分组名。不做目录树：树要处理移动/重命名/级联删除，
    -- 而后台的文件本质是「上传的附件」，扁平 + 分组就够
    group_name    varchar(64)  NOT NULL DEFAULT '',
    original_name varchar(255) NOT NULL,
    storage_path  varchar(255) NOT NULL UNIQUE,
    size          INTEGER      NOT NULL,
    content_type  varchar(128) NOT NULL DEFAULT '',
    uploader_id   INTEGER      NOT NULL DEFAULT 0,
    uploader_name varchar(128) NOT NULL DEFAULT ''
);

CREATE INDEX idx_files_group    ON sys_files(group_name, id);
CREATE INDEX idx_files_uploader ON sys_files(uploader_id, id);
