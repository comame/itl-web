package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/comame/router-go"
	"github.com/hirochachacha/go-smb2"
)

//go:embed front/dist
var distFs embed.FS

func main() {
	conn, err := net.Dial("tcp", "d1.comame.dev:445") // めんどいのでハードコード
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     "read-itunes",
			Password: "read-itunes",
		},
	}

	s, err := d.Dial(conn)
	if err != nil {
		panic(err)
	}
	defer s.Logoff()

	smbfs, err := s.Mount("iTunes")
	if err != nil {
		panic(err)
	}
	defer smbfs.Umount()

	flib, err := smbfs.Open("./iTunes Music Library.xml")
	if err != nil {
		panic(err)
	}
	defer flib.Close()

	tracks, playlists, err := parseLibraryXML(flib)
	if err != nil {
		panic(err)
	}

	router.Get("/api/tracks", func(w http.ResponseWriter, r *http.Request) {
		js, err := json.Marshal(tracks)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(js)
	})
	router.Get("/api/playlists", func(w http.ResponseWriter, r *http.Request) {
		js, err := json.Marshal(playlists)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(js)
	})
	router.Get("/api/track/:persistent_id", func(w http.ResponseWriter, r *http.Request) {
		p := router.Params(r)
		persistentID := p["persistent_id"]

		var track *track
		for _, tr := range tracks {
			if tr.PersistentID == persistentID {
				track = &tr
				break
			}
		}

		if track == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		loc, err := convLocation(track.Locaton)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		f, err := smbfs.Open(loc)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer f.Close()

		typ := mime.TypeByExtension(path.Ext(loc))
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Accept-Ranges", "bytes")

		rh := r.Header.Get("Range")
		if rh == "" {
			// Range でなければ普通に返す
			w.WriteHeader(http.StatusOK)
			io.Copy(w, f)
			return
		}

		start, end, err := parseRangeHeader(rh)
		if err != nil {
			// Range のパースに失敗したら普通に返す
			log.Println(err)
			w.WriteHeader(http.StatusOK)
			io.Copy(w, f)
			return
		}

		aud, err := io.ReadAll(f)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if len(aud) < end {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		if end == 0 {
			w.Write(aud)
			return
		}

		if end < start {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		w.Header().Add("Content-Range", fmt.Sprintf("%d-%d/%d", start, end-1, len(aud)))
		w.Header().Add("Content-Length", fmt.Sprintf("%d", end-start))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(aud[start:end])
	})
	router.Get("/api/artwork/:persistent_id", func(w http.ResponseWriter, r *http.Request) {
		p := router.Params(r)
		persistentID := p["persistent_id"]

		var track *track
		for _, tr := range tracks {
			if tr.PersistentID == persistentID {
				track = &tr
				break
			}
		}

		if track == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		pf, err := extractArtworks(smbfs, *track)
		if err != nil {
			log.Println("ext: " + err.Error())
			w.WriteHeader(http.StatusNotFound)
			return
		}
		defer pf.Close()

		w.WriteHeader(http.StatusOK)
		io.Copy(w, pf)
	})
	router.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		sub, err := fs.Sub(distFs, "front/dist")
		if err != nil {
			panic(err)
		}
		srv := http.FileServer(http.FS(sub))
		srv.ServeHTTP(w, r)
	})

	log.Println("Start http://localhost:8080/")
	http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := os.LookupEnv("DEV")
		if ok {
			log.Println(r.URL.Path)
			allowCORSForDev(w)
		}
		router.Handler().ServeHTTP(w, r)
	}))
}

func parseRangeHeader(v string) (int, int, error) {
	rp := regexp.MustCompile(`^bytes=(\d+)-(\d+)?$`)
	fo := rp.FindAllStringSubmatch(v, 2)
	if fo == nil {
		return 0, 0, errors.New("unsupported or invalid format of range header")
	}

	sstr := fo[0][1]
	estr := "0"

	if len(fo[0]) == 3 && fo[0][2] != "" {
		estr = fo[0][2]
	}

	sint, err := strconv.ParseInt(sstr, 10, 64)
	if err != nil {
		return 0, 0, err
	}
	eint, err := strconv.ParseInt(estr, 10, 64)
	if err != nil {
		return 0, 0, err
	}

	return int(sint), int(eint), nil
}

func extractArtworks(fs *smb2.Share, track track) (io.ReadCloser, error) {
	loc, err := convLocation(track.Locaton)
	if err != nil {
		return nil, err
	}

	afpath := track.PersistentID + path.Ext(loc)

	// とりあえず初回見てみて、存在すればそのまま返す
	fpf, _ := os.Open(".ar/" + afpath + ".jpg")
	if fpf != nil {
		return fpf, nil
	}

	f, err := fs.Open(loc)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// 作業フォルダを作り、
	os.MkdirAll(".ar", 0777)

	// オーディオファイルを開き、もしなければダウンロードし、
	af, err := os.Open(".ar/" + afpath)
	if errors.Is(err, os.ErrNotExist) {
		cf, err := os.Create(".ar/" + afpath)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(cf, f); err != nil {
			return nil, err
		}
		if err := cf.Sync(); err != nil {
			return nil, err
		}
		af = cf
	} else if err != nil {
		return nil, err
	}
	defer af.Close()

	// エラーを握りつぶす。もしここで失敗していたら、次の os.Open でコケるはず
	exec.Command("ffmpeg", "-i", ".ar/"+afpath, "-an", "-c:v", "copy", ".ar/"+afpath+".jpg").Run()

	pf, err := os.Open(".ar/" + afpath + ".jpg")
	if err != nil {
		return nil, err
	}

	return pf, nil
}

func allowCORSForDev(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
}

func convLocation(p string) (string, error) {
	prefix := "file://localhost/C:/Users/comame/Music/iTunes"
	p = "." + p[len(prefix):]
	p = strings.ReplaceAll(p, "+", "%2B")
	p, err := url.QueryUnescape(p)
	if err != nil {
		return "", err
	}
	return p, nil
}

type track struct {
	ID           int
	TotalTime    int
	DiscNumber   int
	DiscCount    int
	TrackNumber  int
	TrackCount   int
	Year         int
	PersistentID string
	Name         string
	Artist       string
	AlbumArtist  string
	Album        string
	Genre        string
	Locaton      string
	Kind         string
}

type playlist struct {
	ID           int
	PersistentID string
	Name         string
	ItemTrackIDs []int
}

// iTunes Music Library.xml をパースする。気合の塊
func parseLibraryXML(r io.Reader) ([]track, []playlist, error) {
	rb := bufio.NewReader(r)
	d := xml.NewDecoder(rb)

	isReadingKey := false

	isReadingTracks := false
	isReadingPlaylists := false

	key := ""
	valueType := "integer" // or "string"

	dictDepth := 0

	var tracks []track
	currentTrack := track{}

	var playlists []playlist
	currentPlaylist := playlist{}

	for {
		rt, err := d.Token()
		if err != nil && errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}

		switch token := rt.(type) {
		case xml.StartElement:
			name := token.Name.Local

			if name == "key" {
				isReadingKey = true
			}

			if name == "dict" || name == "array" {
				dictDepth += 1
			}

			if name == "string" {
				valueType = "string"
			}
			if name == "integer" {
				valueType = "integer"
			}
		case xml.EndElement:
			name := token.Name.Local

			if name == "key" {
				isReadingKey = false
			}

			if name == "dict" || name == "array" {
				dictDepth -= 1

				if isReadingTracks && dictDepth == 2 {
					tracks = append(tracks, currentTrack)
					currentTrack = track{}
				}
				if isReadingPlaylists && dictDepth == 2 {
					playlists = append(playlists, currentPlaylist)
					currentPlaylist = playlist{}
				}
			}

			if name == "integer" || name == "string" {
				valueType = ""
			}
		case xml.CharData:
			strToken := string(token)

			if isReadingKey {
				if !isReadingTracks && !isReadingPlaylists && strToken == "Tracks" {
					isReadingTracks = true
				}
				if isReadingTracks && !isReadingPlaylists && strToken == "Playlists" {
					isReadingTracks = false
					isReadingPlaylists = true
				}
			}

			if isReadingTracks && dictDepth == 3 {
				if isReadingKey {
					key = strToken
				}

				if valueType != "integer" && valueType != "string" {
					continue
				}

				intValue := 0
				if valueType == "integer" {
					v, err := strconv.ParseInt(strToken, 10, 64)
					if err != nil {
						return nil, nil, err
					}
					intValue = int(v)
				}

				switch key {
				case "Track ID":
					currentTrack.ID = intValue
				case "Total Time":
					currentTrack.TotalTime = intValue
				case "Disc Number":
					currentTrack.DiscNumber = intValue
				case "Disc Count":
					currentTrack.DiscCount = intValue
				case "Track Number":
					currentTrack.TrackNumber = intValue
				case "Track Count":
					currentTrack.TrackCount = intValue
				case "Persistent ID":
					currentTrack.PersistentID = strToken
				case "Year":
					currentTrack.Year = intValue
				case "Name":
					currentTrack.Name = strToken
				case "Artist":
					currentTrack.Artist = strToken
				case "Album Artist":
					currentTrack.AlbumArtist = strToken
				case "Album":
					currentTrack.Album = strToken
				case "Genre":
					currentTrack.Genre = strToken
				case "Location":
					currentTrack.Locaton = strToken
				case "Kind":
					currentTrack.Kind = strToken
				}
			}

			if isReadingPlaylists && dictDepth == 3 {
				if isReadingKey {
					key = strToken
				}

				if valueType != "integer" && valueType != "string" {
					continue
				}

				intValue := 0
				if valueType == "integer" {
					v, err := strconv.ParseInt(strToken, 10, 64)
					if err != nil {
						return nil, nil, err
					}
					intValue = int(v)
				}

				switch key {
				case "Playlist ID":
					currentPlaylist.ID = intValue
				case "Playlist Persistent ID":
					currentPlaylist.PersistentID = strToken
				case "Name":
					currentPlaylist.Name = strToken
				}
			}

			if isReadingPlaylists && dictDepth == 5 {
				if valueType != "integer" {
					continue
				}
				v, err := strconv.ParseInt(strToken, 10, 64)
				if err != nil {
					return nil, nil, err
				}
				currentPlaylist.ItemTrackIDs = append(currentPlaylist.ItemTrackIDs, int(v))
			}
		}
	}

	return tracks, playlists, nil
}
