package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedImageLibrary 挂载资源中心图片的 `/api/v1` 入口。
// 所有入口都要求登录；图片库是当前安装实例共享的一份素材，不按闲鱼账号隔离。
func (s *Server) mountVersionedImageLibrary(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)

		r.Get("/api/v1/image-library", s.listImageLibrary)
		r.Post("/api/v1/image-library", s.uploadImageLibrary)
		r.Patch("/api/v1/image-library/{asset_id}", s.renameImageLibrary)
		r.Delete("/api/v1/image-library/{asset_id}", s.deleteImageLibrary)
		r.Get("/api/v1/image-library/{asset_id}/content", s.imageLibraryContent)
	})
}
