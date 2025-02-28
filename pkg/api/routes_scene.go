package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/manager"
	"github.com/stashapp/stash/pkg/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

type sceneRoutes struct {
	txnManager models.TransactionManager
}

func (rs sceneRoutes) Routes() chi.Router {
	r := chi.NewRouter()

	r.Route("/{sceneId}", func(r chi.Router) {
		r.Use(SceneCtx)

		// streaming endpoints
		r.Get("/stream", rs.StreamDirect)
		r.Get("/stream.mkv", rs.StreamMKV)
		r.Get("/stream.webm", rs.StreamWebM)
		r.Get("/stream.m3u8", rs.StreamHLS)
		r.Get("/stream.ts", rs.StreamTS)
		r.Get("/stream.mp4", rs.StreamMp4)

		r.Get("/screenshot", rs.Screenshot)
		r.Get("/preview", rs.Preview)
		r.Get("/webp", rs.Webp)
		r.Get("/vtt/chapter", rs.ChapterVtt)
		r.Get("/funscript", rs.Funscript)

		r.Get("/scene_marker/{sceneMarkerId}/stream", rs.SceneMarkerStream)
		r.Get("/scene_marker/{sceneMarkerId}/preview", rs.SceneMarkerPreview)
		r.Get("/scene_marker/{sceneMarkerId}/screenshot", rs.SceneMarkerScreenshot)
	})
	r.With(SceneCtx).Get("/{sceneId}_thumbs.vtt", rs.VttThumbs)
	r.With(SceneCtx).Get("/{sceneId}_sprite.jpg", rs.VttSprite)

	return r
}

// region Handlers

func getSceneFileContainer(scene *models.Scene) ffmpeg.Container {
	var container ffmpeg.Container
	if scene.Format.Valid {
		container = ffmpeg.Container(scene.Format.String)
	} else { // container isn't in the DB
		// shouldn't happen, fallback to ffprobe
		tmpVideoFile, err := ffmpeg.NewVideoFile(manager.GetInstance().FFProbePath, scene.Path, false)
		if err != nil {
			logger.Errorf("[transcode] error reading video file: %s", err.Error())
			return ffmpeg.Container("")
		}

		container = ffmpeg.MatchContainer(tmpVideoFile.Container, scene.Path)
	}

	return container
}

func (rs sceneRoutes) StreamDirect(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	ss := manager.SceneServer{
		TXNManager: rs.txnManager,
	}
	ss.StreamSceneDirect(scene, w, r)
}

func (rs sceneRoutes) StreamMKV(w http.ResponseWriter, r *http.Request) {
	// only allow mkv streaming if the scene container is an mkv already
	scene := r.Context().Value(sceneKey).(*models.Scene)

	container := getSceneFileContainer(scene)
	if container != ffmpeg.Matroska {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("not an mkv file"))
		return
	}

	rs.streamTranscode(w, r, ffmpeg.CodecMKVAudio)
}

func (rs sceneRoutes) StreamWebM(w http.ResponseWriter, r *http.Request) {
	rs.streamTranscode(w, r, ffmpeg.CodecVP9)
}

func (rs sceneRoutes) StreamMp4(w http.ResponseWriter, r *http.Request) {
	rs.streamTranscode(w, r, ffmpeg.CodecH264)
}

func (rs sceneRoutes) StreamHLS(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	videoFile, err := ffmpeg.NewVideoFile(manager.GetInstance().FFProbePath, scene.Path, false)
	if err != nil {
		logger.Errorf("[stream] error reading video file: %s", err.Error())
		return
	}

	logger.Debug("Returning HLS playlist")

	// getting the playlist manifest only
	w.Header().Set("Content-Type", ffmpeg.MimeHLS)
	var str strings.Builder

	ffmpeg.WriteHLSPlaylist(*videoFile, r.URL.String(), &str)

	requestByteRange := utils.CreateByteRange(r.Header.Get("Range"))
	if requestByteRange.RawString != "" {
		logger.Debugf("Requested range: %s", requestByteRange.RawString)
	}

	ret := requestByteRange.Apply([]byte(str.String()))
	rangeStr := requestByteRange.ToHeaderValue(int64(str.Len()))
	w.Header().Set("Content-Range", rangeStr)

	w.Write(ret)
}

func (rs sceneRoutes) StreamTS(w http.ResponseWriter, r *http.Request) {
	rs.streamTranscode(w, r, ffmpeg.CodecHLS)
}

func (rs sceneRoutes) streamTranscode(w http.ResponseWriter, r *http.Request, videoCodec ffmpeg.Codec) {
	logger.Debugf("Streaming as %s", videoCodec.MimeType)
	scene := r.Context().Value(sceneKey).(*models.Scene)

	// needs to be transcoded

	videoFile, err := ffmpeg.NewVideoFile(manager.GetInstance().FFProbePath, scene.Path, false)
	if err != nil {
		logger.Errorf("[stream] error reading video file: %s", err.Error())
		return
	}

	// start stream based on query param, if provided
	r.ParseForm()
	startTime := r.Form.Get("start")
	requestedSize := r.Form.Get("resolution")

	var stream *ffmpeg.Stream

	audioCodec := ffmpeg.MissingUnsupported
	if scene.AudioCodec.Valid {
		audioCodec = ffmpeg.AudioCodec(scene.AudioCodec.String)
	}

	options := ffmpeg.GetTranscodeStreamOptions(*videoFile, videoCodec, audioCodec)
	options.StartTime = startTime
	options.MaxTranscodeSize = config.GetInstance().GetMaxStreamingTranscodeSize()
	if requestedSize != "" {
		options.MaxTranscodeSize = models.StreamingResolutionEnum(requestedSize)
	}

	encoder := ffmpeg.NewEncoder(manager.GetInstance().FFMPEGPath)
	stream, err = encoder.GetTranscodeStream(options)

	if err != nil {
		logger.Errorf("[stream] error transcoding video file: %s", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	stream.Serve(w, r)
}

func (rs sceneRoutes) Screenshot(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)

	ss := manager.SceneServer{
		TXNManager: rs.txnManager,
	}
	ss.ServeScreenshot(scene, w, r)
}

func (rs sceneRoutes) Preview(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	filepath := manager.GetInstance().Paths.Scene.GetStreamPreviewPath(scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm()))
	utils.ServeFileNoCache(w, r, filepath)
}

func (rs sceneRoutes) Webp(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	filepath := manager.GetInstance().Paths.Scene.GetStreamPreviewImagePath(scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm()))
	http.ServeFile(w, r, filepath)
}

func (rs sceneRoutes) getChapterVttTitle(ctx context.Context, marker *models.SceneMarker) string {
	if marker.Title != "" {
		return marker.Title
	}

	var ret string
	if err := rs.txnManager.WithReadTxn(ctx, func(repo models.ReaderRepository) error {
		qb := repo.Tag()
		primaryTag, err := qb.Find(marker.PrimaryTagID)
		if err != nil {
			return err
		}

		ret = primaryTag.Name

		tags, err := qb.FindBySceneMarkerID(marker.ID)
		if err != nil {
			return err
		}

		for _, t := range tags {
			ret += ", " + t.Name
		}

		return nil
	}); err != nil {
		panic(err)
	}

	return ret
}

func (rs sceneRoutes) ChapterVtt(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	var sceneMarkers []*models.SceneMarker
	if err := rs.txnManager.WithReadTxn(r.Context(), func(repo models.ReaderRepository) error {
		var err error
		sceneMarkers, err = repo.SceneMarker().FindBySceneID(scene.ID)
		return err
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	vttLines := []string{"WEBVTT", ""}
	for i, marker := range sceneMarkers {
		vttLines = append(vttLines, strconv.Itoa(i+1))
		time := utils.GetVTTTime(marker.Seconds)
		vttLines = append(vttLines, time+" --> "+time)
		vttLines = append(vttLines, rs.getChapterVttTitle(r.Context(), marker))
		vttLines = append(vttLines, "")
	}
	vtt := strings.Join(vttLines, "\n")

	w.Header().Set("Content-Type", "text/vtt")
	_, _ = w.Write([]byte(vtt))
}

func (rs sceneRoutes) Funscript(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	funscript := utils.GetFunscriptPath(scene.Path)
	utils.ServeFileNoCache(w, r, funscript)
}

func (rs sceneRoutes) VttThumbs(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	w.Header().Set("Content-Type", "text/vtt")
	filepath := manager.GetInstance().Paths.Scene.GetSpriteVttFilePath(scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm()))
	http.ServeFile(w, r, filepath)
}

func (rs sceneRoutes) VttSprite(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	w.Header().Set("Content-Type", "image/jpeg")
	filepath := manager.GetInstance().Paths.Scene.GetSpriteImageFilePath(scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm()))
	http.ServeFile(w, r, filepath)
}

func (rs sceneRoutes) SceneMarkerStream(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	sceneMarkerID, _ := strconv.Atoi(chi.URLParam(r, "sceneMarkerId"))
	var sceneMarker *models.SceneMarker
	if err := rs.txnManager.WithReadTxn(r.Context(), func(repo models.ReaderRepository) error {
		var err error
		sceneMarker, err = repo.SceneMarker().Find(sceneMarkerID)
		return err
	}); err != nil {
		logger.Warnf("Error when getting scene marker for stream: %s", err.Error())
		http.Error(w, http.StatusText(500), 500)
		return
	}
	filepath := manager.GetInstance().Paths.SceneMarkers.GetStreamPath(scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm()), int(sceneMarker.Seconds))
	http.ServeFile(w, r, filepath)
}

func (rs sceneRoutes) SceneMarkerPreview(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	sceneMarkerID, _ := strconv.Atoi(chi.URLParam(r, "sceneMarkerId"))
	var sceneMarker *models.SceneMarker
	if err := rs.txnManager.WithReadTxn(r.Context(), func(repo models.ReaderRepository) error {
		var err error
		sceneMarker, err = repo.SceneMarker().Find(sceneMarkerID)
		return err
	}); err != nil {
		logger.Warnf("Error when getting scene marker for stream: %s", err.Error())
		http.Error(w, http.StatusText(500), 500)
		return
	}
	filepath := manager.GetInstance().Paths.SceneMarkers.GetStreamPreviewImagePath(scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm()), int(sceneMarker.Seconds))

	// If the image doesn't exist, send the placeholder
	exists, _ := utils.FileExists(filepath)
	if !exists {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(utils.PendingGenerateResource)
		return
	}

	http.ServeFile(w, r, filepath)
}

func (rs sceneRoutes) SceneMarkerScreenshot(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	sceneMarkerID, _ := strconv.Atoi(chi.URLParam(r, "sceneMarkerId"))
	var sceneMarker *models.SceneMarker
	if err := rs.txnManager.WithReadTxn(r.Context(), func(repo models.ReaderRepository) error {
		var err error
		sceneMarker, err = repo.SceneMarker().Find(sceneMarkerID)
		return err
	}); err != nil {
		logger.Warnf("Error when getting scene marker for stream: %s", err.Error())
		http.Error(w, http.StatusText(500), 500)
		return
	}
	filepath := manager.GetInstance().Paths.SceneMarkers.GetStreamScreenshotPath(scene.GetHash(config.GetInstance().GetVideoFileNamingAlgorithm()), int(sceneMarker.Seconds))

	// If the image doesn't exist, send the placeholder
	exists, _ := utils.FileExists(filepath)
	if !exists {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(utils.PendingGenerateResource)
		return
	}

	http.ServeFile(w, r, filepath)
}

// endregion

func SceneCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sceneIdentifierQueryParam := chi.URLParam(r, "sceneId")
		sceneID, _ := strconv.Atoi(sceneIdentifierQueryParam)

		var scene *models.Scene
		manager.GetInstance().TxnManager.WithReadTxn(r.Context(), func(repo models.ReaderRepository) error {
			qb := repo.Scene()
			if sceneID == 0 {
				// determine checksum/os by the length of the query param
				if len(sceneIdentifierQueryParam) == 32 {
					scene, _ = qb.FindByChecksum(sceneIdentifierQueryParam)
				} else {
					scene, _ = qb.FindByOSHash(sceneIdentifierQueryParam)
				}
			} else {
				scene, _ = qb.Find(sceneID)
			}

			return nil
		})

		if scene == nil {
			http.Error(w, http.StatusText(404), 404)
			return
		}

		ctx := context.WithValue(r.Context(), sceneKey, scene)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
