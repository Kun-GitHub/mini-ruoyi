package domain

import "time"

// File 是一条文件记录。文件本体在磁盘上，这里只有元数据。
type File struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	GroupName string    `json:"group_name"`
	// OriginalName 是用户给的文件名，只用于展示与下载时的文件名。
	OriginalName string `json:"original_name"`
	// StoragePath 是磁盘上的相对路径，**不出现在响应里**：
	// 它属于内部实现，暴露出去只会招来「能不能直接拼 URL 下载」的尝试。
	StoragePath  string `json:"-"`
	Size         int64  `json:"size"`
	ContentType  string `json:"content_type"`
	UploaderID   int64  `json:"uploader_id"`
	UploaderName string `json:"uploader_name"`
}

// FileUsage 是容量用量，给界面显示与上传前检查用。
type FileUsage struct {
	Used  int64 `json:"used"`
	Quota int64 `json:"quota"`
	Count int64 `json:"count"`
	// MaxSize 是单文件上限。放在这里是为了让界面显示真实数字——
	// 前端自己写一个常量迟早会和后端配置对不上。
	MaxSize int64 `json:"max_size"`
}
