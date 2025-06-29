package http

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sjzar/chatlog/internal/errors"
	"github.com/sjzar/chatlog/pkg/util"
	"github.com/sjzar/chatlog/pkg/util/dat2img"
	"github.com/sjzar/chatlog/pkg/util/silk"

	"github.com/gin-gonic/gin"
)

// EFS holds embedded file system data for static assets.
//
//go:embed static
var EFS embed.FS

func (s *Service) initRouter() {

	router := s.GetRouter()

	staticDir, _ := fs.Sub(EFS, "static")
	router.StaticFS("/static", http.FS(staticDir))
	router.StaticFileFS("/favicon.ico", "./favicon.ico", http.FS(staticDir))
	router.StaticFileFS("/", "./index.htm", http.FS(staticDir))

	// Media
	router.GET("/image/*key", s.GetImage)
	router.GET("/video/*key", s.GetVideo)
	router.GET("/file/*key", s.GetFile)
	router.GET("/voice/*key", s.GetVoice)
	router.GET("/data/*path", s.GetMediaData)

	// MCP Server
	{
		router.GET("/sse", s.mcp.HandleSSE)
		router.POST("/messages", s.mcp.HandleMessages)
		// mcp inspector is shit
		// https://github.com/modelcontextprotocol/inspector/blob/aeaf32f/server/src/index.ts#L155
		router.POST("/message", s.mcp.HandleMessages)
	}

	// API V1 Router
	api := router.Group("/api/v1")
	{
		api.GET("/chatlog", s.GetChatlog)
		api.GET("/contact", s.GetContacts)
		api.GET("/chatroom", s.GetChatRooms)
		api.GET("/session", s.GetSessions)
		api.GET("/analysis/report", s.GetAnalysisReport)
		api.GET("/analysis/stats", s.GetAnalysisStats)
		api.GET("/analysis/export", s.ExportAnalysisData)
		api.GET("/analysis/files", s.GetAnalysisFiles)
		api.GET("/analysis/download", s.DownloadAnalysisFile)
		api.GET("/analysis/search", s.SearchMessages)
		api.GET("/analysis/chatroom", s.GetChatroomHistory)
		api.GET("/analysis/daily-summary", s.GetDailySummary)
		api.GET("/analysis/golden-quotes", s.GetGoldenQuotes)
	}

	router.NoRoute(s.NoRoute)
}

// NoRoute handles 404 Not Found errors. If the request URL starts with "/api"
// or "/static", it responds with a JSON error. Otherwise, it redirects to the root path.
func (s *Service) NoRoute(c *gin.Context) {
	path := c.Request.URL.Path
	switch {
	case strings.HasPrefix(path, "/api"), strings.HasPrefix(path, "/static"):
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
	default:
		c.Header("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate, value")
		c.Redirect(http.StatusFound, "/")
	}
}

func (s *Service) GetChatlog(c *gin.Context) {

	q := struct {
		Time    string `form:"time"`
		Talker  string `form:"talker"`
		Sender  string `form:"sender"`
		Keyword string `form:"keyword"`
		Limit   int    `form:"limit"`
		Offset  int    `form:"offset"`
		Format  string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	var err error
	start, end, ok := util.TimeRangeOf(q.Time)
	if !ok {
		errors.Err(c, errors.InvalidArg("time"))
	}
	if q.Limit < 0 {
		q.Limit = 0
	}

	if q.Offset < 0 {
		q.Offset = 0
	}

	messages, err := s.db.GetMessages(start, end, q.Talker, q.Sender, q.Keyword, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}

	switch strings.ToLower(q.Format) {
	case "csv":
	case "json":
		// json
		c.JSON(http.StatusOK, messages)
	default:
		// plain text
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		for _, m := range messages {
			c.Writer.WriteString(m.PlainText(strings.Contains(q.Talker, ","), util.PerfectTimeFormat(start, end), c.Request.Host))
			c.Writer.WriteString("\n")
			c.Writer.Flush()
		}
	}
}

func (s *Service) GetContacts(c *gin.Context) {

	q := struct {
		Keyword string `form:"keyword"`
		Limit   int    `form:"limit"`
		Offset  int    `form:"offset"`
		Format  string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	list, err := s.db.GetContacts(q.Keyword, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}

	format := strings.ToLower(q.Format)
	switch format {
	case "json":
		// json
		c.JSON(http.StatusOK, list)
	default:
		// csv
		if format == "csv" {
			// 浏览器访问时，会下载文件
			c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		} else {
			c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		c.Writer.WriteString("UserName,Alias,Remark,NickName\n")
		for _, contact := range list.Items {
			c.Writer.WriteString(fmt.Sprintf("%s,%s,%s,%s\n", contact.UserName, contact.Alias, contact.Remark, contact.NickName))
		}
		c.Writer.Flush()
	}
}

func (s *Service) GetChatRooms(c *gin.Context) {

	q := struct {
		Keyword string `form:"keyword"`
		Limit   int    `form:"limit"`
		Offset  int    `form:"offset"`
		Format  string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	list, err := s.db.GetChatRooms(q.Keyword, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}
	format := strings.ToLower(q.Format)
	switch format {
	case "json":
		// json
		c.JSON(http.StatusOK, list)
	default:
		// csv
		if format == "csv" {
			// 浏览器访问时，会下载文件
			c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		} else {
			c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		c.Writer.WriteString("Name,Remark,NickName,Owner,UserCount\n")
		for _, chatRoom := range list.Items {
			c.Writer.WriteString(fmt.Sprintf("%s,%s,%s,%s,%d\n", chatRoom.Name, chatRoom.Remark, chatRoom.NickName, chatRoom.Owner, len(chatRoom.Users)))
		}
		c.Writer.Flush()
	}
}

func (s *Service) GetSessions(c *gin.Context) {

	q := struct {
		Keyword string `form:"keyword"`
		Limit   int    `form:"limit"`
		Offset  int    `form:"offset"`
		Format  string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	sessions, err := s.db.GetSessions(q.Keyword, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}
	format := strings.ToLower(q.Format)
	switch format {
	case "csv":
		c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		c.Writer.WriteString("UserName,NOrder,NickName,Content,NTime\n")
		for _, session := range sessions.Items {
			c.Writer.WriteString(fmt.Sprintf("%s,%d,%s,%s,%s\n", session.UserName, session.NOrder, session.NickName, strings.ReplaceAll(session.Content, "\n", "\\n"), session.NTime))
		}
		c.Writer.Flush()
	case "json":
		// json
		c.JSON(http.StatusOK, sessions)
	default:
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()
		for _, session := range sessions.Items {
			c.Writer.WriteString(session.PlainText(120))
			c.Writer.WriteString("\n")
		}
		c.Writer.Flush()
	}
}

func (s *Service) GetImage(c *gin.Context) {
	s.GetMedia(c, "image")
}

func (s *Service) GetVideo(c *gin.Context) {
	s.GetMedia(c, "video")
}

func (s *Service) GetFile(c *gin.Context) {
	s.GetMedia(c, "file")
}
func (s *Service) GetVoice(c *gin.Context) {
	s.GetMedia(c, "voice")
}

func (s *Service) GetMedia(c *gin.Context, _type string) {
	key := strings.TrimPrefix(c.Param("key"), "/")
	if key == "" {
		errors.Err(c, errors.InvalidArg(key))
		return
	}

	keys := util.Str2List(key, ",")
	if len(keys) == 0 {
		errors.Err(c, errors.InvalidArg(key))
		return
	}

	var _err error
	for _, k := range keys {
		if len(k) != 32 {
			absolutePath := filepath.Join(s.ctx.DataDir, k)
			if _, err := os.Stat(absolutePath); os.IsNotExist(err) {
				continue
			}
			c.Redirect(http.StatusFound, "/data/"+k)
			return
		}
		media, err := s.db.GetMedia(_type, k)
		if err != nil {
			_err = err
			continue
		}
		if c.Query("info") != "" {
			c.JSON(http.StatusOK, media)
			return
		}
		switch media.Type {
		case "voice":
			s.HandleVoice(c, media.Data)
			return
		default:
			c.Redirect(http.StatusFound, "/data/"+media.Path)
			return
		}
	}

	if _err != nil {
		errors.Err(c, _err)
		return
	}
}

func (s *Service) GetMediaData(c *gin.Context) {
	relativePath := filepath.Clean(c.Param("path"))

	absolutePath := filepath.Join(s.ctx.DataDir, relativePath)

	if _, err := os.Stat(absolutePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "File not found",
		})
		return
	}

	ext := strings.ToLower(filepath.Ext(absolutePath))
	switch {
	case ext == ".dat":
		s.HandleDatFile(c, absolutePath)
	default:
		// 直接返回文件
		c.File(absolutePath)
	}

}

func (s *Service) HandleDatFile(c *gin.Context, path string) {

	b, err := os.ReadFile(path)
	if err != nil {
		errors.Err(c, err)
		return
	}
	out, ext, err := dat2img.Dat2Image(b)
	if err != nil {
		c.File(path)
		return
	}

	switch ext {
	case "jpg":
		c.Data(http.StatusOK, "image/jpeg", out)
	case "png":
		c.Data(http.StatusOK, "image/png", out)
	case "gif":
		c.Data(http.StatusOK, "image/gif", out)
	case "bmp":
		c.Data(http.StatusOK, "image/bmp", out)
	default:
		c.Data(http.StatusOK, "image/jpg", out)
		// c.File(path)
	}
}

func (s *Service) HandleVoice(c *gin.Context, data []byte) {
	out, err := silk.Silk2MP3(data)
	if err != nil {
		c.Data(http.StatusOK, "audio/silk", data)
		return
	}
	c.Data(http.StatusOK, "audio/mp3", out)
}

// GetAnalysisReport 获取分析报告
func (s *Service) GetAnalysisReport(c *gin.Context) {
	// 查找最新的分析报告文件
	pattern := "wechat_report_*.json"
	matches, err := filepath.Glob(pattern)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find report files"})
		return
	}

	if len(matches) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No analysis report found"})
		return
	}

	// 获取最新的报告文件
	latestFile := matches[len(matches)-1]
	data, err := os.ReadFile(latestFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read report file"})
		return
	}

	var report map[string]interface{}
	if err := json.Unmarshal(data, &report); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse report file"})
		return
	}

	c.JSON(http.StatusOK, report)
}

// GetAnalysisStats 获取基础统计信息
func (s *Service) GetAnalysisStats(c *gin.Context) {
	stats := make(map[string]interface{})

	// 统计会话数量
	sessions, err := s.db.GetSessions("", 0, 0)
	if err == nil {
		stats["total_sessions"] = len(sessions.Items)
	}

	// 统计联系人数量
	contacts, err := s.db.GetContacts("", 0, 0)
	if err == nil {
		stats["total_contacts"] = len(contacts.Items)
	}

	// 统计群聊数量
	chatrooms, err := s.db.GetChatRooms("", 0, 0)
	if err == nil {
		stats["total_chatrooms"] = len(chatrooms.Items)
	}

	// 统计最近7天的消息数量
	end := time.Now()
	start := end.AddDate(0, 0, -7)
	messages, err := s.db.GetMessages(start, end, "", "", "", 0, 0)
	if err == nil {
		stats["recent_messages"] = len(messages)
	}

	// 添加时间戳
	stats["generated_at"] = time.Now().Format("2006-01-02 15:04:05")

	c.JSON(http.StatusOK, stats)
}

// ExportAnalysisData 导出分析数据
func (s *Service) ExportAnalysisData(c *gin.Context) {
	exportType := c.Query("type")
	
	switch exportType {
	case "sessions":
		sessions, err := s.db.GetSessions("", 0, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get sessions"})
			return
		}
		
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", "attachment; filename=sessions_export.csv")
		
		c.Writer.WriteString("UserName,NOrder,NickName,Content,NTime\n")
		for _, session := range sessions.Items {
			c.Writer.WriteString(fmt.Sprintf("%s,%d,%s,%s,%s\n", 
				session.UserName, session.NOrder, session.NickName, 
				strings.ReplaceAll(session.Content, "\n", "\\n"), session.NTime))
		}
		
	case "contacts":
		contacts, err := s.db.GetContacts("", 0, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get contacts"})
			return
		}
		
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", "attachment; filename=contacts_export.csv")
		
		c.Writer.WriteString("UserName,Alias,Remark,NickName\n")
		for _, contact := range contacts.Items {
			c.Writer.WriteString(fmt.Sprintf("%s,%s,%s,%s\n", 
				contact.UserName, contact.Alias, contact.Remark, contact.NickName))
		}
		
	case "chatrooms":
		chatrooms, err := s.db.GetChatRooms("", 0, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get chatrooms"})
			return
		}
		
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", "attachment; filename=chatrooms_export.csv")
		
		c.Writer.WriteString("Name,Remark,NickName,Owner,UserCount\n")
		for _, chatroom := range chatrooms.Items {
			c.Writer.WriteString(fmt.Sprintf("%s,%s,%s,%s,%d\n", 
				chatroom.Name, chatroom.Remark, chatroom.NickName, chatroom.Owner, len(chatroom.Users)))
		}
		
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid export type"})
	}
}

// GetAnalysisFiles 获取可下载的分析文件列表
func (s *Service) GetAnalysisFiles(c *gin.Context) {
	files := []map[string]string{}
	
	// 查找分析报告文件
	pattern := "wechat_report_*.json"
	matches, err := filepath.Glob(pattern)
	if err == nil {
		for _, match := range matches {
			info, err := os.Stat(match)
			if err == nil {
				files = append(files, map[string]string{
					"name": match,
					"size": fmt.Sprintf("%.2f KB", float64(info.Size())/1024),
					"type": "JSON Report",
					"url":  "/api/v1/analysis/download?file=" + match,
				})
			}
		}
	}
	
	// 查找导出目录
	exportDirs, err := filepath.Glob("wechat_export_*")
	if err == nil {
		for _, dir := range exportDirs {
			info, err := os.Stat(dir)
			if err == nil && info.IsDir() {
				files = append(files, map[string]string{
					"name": dir,
					"size": "Directory",
					"type": "Export Folder",
					"url":  "/api/v1/analysis/download?folder=" + dir,
				})
			}
		}
	}
	
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// DownloadAnalysisFile 下载分析文件
func (s *Service) DownloadAnalysisFile(c *gin.Context) {
	file := c.Query("file")
	folder := c.Query("folder")
	
	if file != "" {
		// 下载单个文件
		if _, err := os.Stat(file); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
			return
		}
		c.File(file)
		return
	}
	
	if folder != "" {
		// 下载整个文件夹（压缩）
		if _, err := os.Stat(folder); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Folder not found"})
			return
		}
		
		// 这里可以添加压缩功能，暂时直接返回文件夹信息
		c.JSON(http.StatusOK, gin.H{
			"message": "Folder download not implemented yet",
			"folder":  folder,
		})
		return
	}
	
	c.JSON(http.StatusBadRequest, gin.H{"error": "No file or folder specified"})
}

// SearchMessages 搜索消息
func (s *Service) SearchMessages(c *gin.Context) {
	keyword := c.Query("keyword")
	days := c.DefaultQuery("days", "7")
	
	if keyword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Keyword is required"})
		return
	}
	
	// 计算时间范围
	end := time.Now()
	daysInt := 7
	if d, err := strconv.Atoi(days); err == nil && d > 0 {
		daysInt = d
	}
	start := end.AddDate(0, 0, -daysInt)
	
	// 搜索消息
	messages, err := s.db.GetMessages(start, end, "", "", keyword, 1000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search messages"})
		return
	}
	
	// 按群聊分组
	groupedMessages := make(map[string][]interface{})
	for _, msg := range messages {
		groupKey := msg.Talker
		if groupKey == "" {
			groupKey = "未知群聊"
		}
		
		msgData := map[string]interface{}{
			"content":    msg.Content,
			"time":       msg.Time.Unix(),
			"sender":     msg.Sender,
			"talker":     msg.Talker,
			"type":       msg.Type,
		}
		
		groupedMessages[groupKey] = append(groupedMessages[groupKey], msgData)
	}
	
	result := map[string]interface{}{
		"keyword":         keyword,
		"search_days":     daysInt,
		"total_messages":  len(messages),
		"grouped_results": groupedMessages,
		"search_time":     time.Now().Format("2006-01-02 15:04:05"),
	}
	
	c.JSON(http.StatusOK, result)
}

// GetChatroomHistory 获取特定群聊的历史记录
func (s *Service) GetChatroomHistory(c *gin.Context) {
	talker := c.Query("talker")
	days := c.DefaultQuery("days", "30")
	
	if talker == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Talker is required"})
		return
	}
	
	// 计算时间范围
	end := time.Now()
	daysInt := 30
	if d, err := strconv.Atoi(days); err == nil && d > 0 {
		daysInt = d
	}
	start := end.AddDate(0, 0, -daysInt)
	
	// 获取群聊消息
	messages, err := s.db.GetMessages(start, end, talker, "", "", 5000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get chatroom history"})
		return
	}
	
	// 按日期分组
	dailyMessages := make(map[string][]interface{})
	for _, msg := range messages {
		date := msg.Time.Format("2006-01-02")
		
		msgData := map[string]interface{}{
			"content":    msg.Content,
			"time":       msg.Time.Unix(),
			"sender":     msg.Sender,
			"type":       msg.Type,
			"hour":       msg.Time.Hour(),
		}
		
		dailyMessages[date] = append(dailyMessages[date], msgData)
	}
	
	// 统计信息
	stats := map[string]interface{}{
		"total_messages": len(messages),
		"total_days":     len(dailyMessages),
		"start_date":     start.Format("2006-01-02"),
		"end_date":       end.Format("2006-01-02"),
	}
	
	result := map[string]interface{}{
		"talker":         talker,
		"stats":          stats,
		"daily_messages": dailyMessages,
		"query_time":     time.Now().Format("2006-01-02 15:04:05"),
	}
	
	c.JSON(http.StatusOK, result)
}

// GetDailySummary 获取每日群聊内容主题汇总
func (s *Service) GetDailySummary(c *gin.Context) {
	date := c.DefaultQuery("date", time.Now().Format("2006-01-02"))
	talker := c.Query("talker") // 可选，指定群聊
	
	// 解析日期
	targetDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format"})
		return
	}
	
	start := targetDate
	end := targetDate.AddDate(0, 0, 1)
	
	// 获取当日消息
	messages, err := s.db.GetMessages(start, end, talker, "", "", 10000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get daily messages"})
		return
	}
	
	// 按群聊分组
	groupedMessages := make(map[string][]string)
	for _, msg := range messages {
		if msg.Type == 1 && msg.Content != "" { // 只处理文本消息
			groupKey := msg.Talker
			if groupKey == "" {
				groupKey = "未知群聊"
			}
			groupedMessages[groupKey] = append(groupedMessages[groupKey], msg.Content)
		}
	}
	
	// 生成主题汇总
	dailySummaries := make(map[string]interface{})
	for groupName, contents := range groupedMessages {
		summary := generateTopicSummary(contents)
		dailySummaries[groupName] = map[string]interface{}{
			"message_count": len(contents),
			"topics":        summary.topics,
			"keywords":      summary.keywords,
			"activity_level": getActivityLevel(len(contents)),
		}
	}
	
	result := map[string]interface{}{
		"date":           date,
		"total_groups":   len(groupedMessages),
		"total_messages": len(messages),
		"summaries":      dailySummaries,
		"generated_at":   time.Now().Format("2006-01-02 15:04:05"),
	}
	
	c.JSON(http.StatusOK, result)
}

// GetGoldenQuotes 获取每日金句
func (s *Service) GetGoldenQuotes(c *gin.Context) {
	date := c.DefaultQuery("date", time.Now().Format("2006-01-02"))
	talker := c.Query("talker") // 可选，指定群聊
	
	// 解析日期
	targetDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format"})
		return
	}
	
	start := targetDate
	end := targetDate.AddDate(0, 0, 1)
	
	// 获取当日消息
	messages, err := s.db.GetMessages(start, end, talker, "", "", 10000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get daily messages"})
		return
	}
	
	// 提取文本消息
	var textMessages []string
	for _, msg := range messages {
		if msg.Type == 1 && msg.Content != "" && len(msg.Content) > 10 {
			textMessages = append(textMessages, msg.Content)
		}
	}
	
	// 生成金句
	goldenQuotes := extractGoldenQuotes(textMessages)
	
	result := map[string]interface{}{
		"date":         date,
		"talker":       talker,
		"total_quotes": len(goldenQuotes),
		"quotes":       goldenQuotes,
		"generated_at": time.Now().Format("2006-01-02 15:04:05"),
	}
	
	c.JSON(http.StatusOK, result)
}

// 辅助结构体
type topicSummary struct {
	topics   []string
	keywords []string
}

// generateTopicSummary 生成主题汇总
func generateTopicSummary(contents []string) topicSummary {
	// 简单的关键词提取逻辑
	keywords := []string{}
	topics := []string{}
	
	// 统计高频词汇
	wordCount := make(map[string]int)
	for _, content := range contents {
		words := strings.Fields(content)
		for _, word := range words {
			if len(word) > 1 {
				wordCount[word]++
			}
		}
	}
	
	// 提取高频关键词
	for word, count := range wordCount {
		if count >= 3 && len(word) > 1 {
			keywords = append(keywords, word)
		}
	}
	
	// 限制关键词数量
	if len(keywords) > 10 {
		keywords = keywords[:10]
	}
	
	// 生成主题
	if len(contents) > 0 {
		topics = append(topics, "日常交流")
		if len(keywords) > 5 {
			topics = append(topics, "热门话题讨论")
		}
		if len(contents) > 100 {
			topics = append(topics, "活跃群聊")
		}
	}
	
	return topicSummary{
		topics:   topics,
		keywords: keywords,
	}
}

// getActivityLevel 获取活跃度等级
func getActivityLevel(messageCount int) string {
	switch {
	case messageCount >= 100:
		return "🔥 非常活跃"
	case messageCount >= 50:
		return "⚡ 活跃"
	case messageCount >= 20:
		return "📈 一般"
	case messageCount >= 10:
		return "📊 较少"
	default:
		return "😴 安静"
	}
}

// extractGoldenQuotes 提取金句
func extractGoldenQuotes(messages []string) []map[string]interface{} {
	var quotes []map[string]interface{}
	
	// 简单的金句提取逻辑
	for i, msg := range messages {
		// 筛选可能成为金句的消息
		if len(msg) > 15 && len(msg) < 200 {
			// 检查是否包含特殊符号或表情
			if strings.Contains(msg, "！") || strings.Contains(msg, "？") || 
			   strings.Contains(msg, "💡") || strings.Contains(msg, "🌟") ||
			   strings.Contains(msg, "金句") || strings.Contains(msg, "经典") {
				
				quotes = append(quotes, map[string]interface{}{
					"content": msg,
					"index":   i + 1,
					"length":  len(msg),
				})
			}
		}
	}
	
	// 如果金句不够，选择一些较长的消息
	if len(quotes) < 10 {
		for i, msg := range messages {
			if len(msg) > 30 && len(quotes) < 10 {
				// 避免重复
				isDuplicate := false
				for _, quote := range quotes {
					if quote["content"] == msg {
						isDuplicate = true
						break
					}
				}
				
				if !isDuplicate {
					quotes = append(quotes, map[string]interface{}{
						"content": msg,
						"index":   i + 1,
						"length":  len(msg),
					})
				}
			}
		}
	}
	
	// 限制数量
	if len(quotes) > 10 {
		quotes = quotes[:10]
	}
	
	return quotes
}
