package main

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/classroom/v1"
	"google.golang.org/api/option"
	"log"
	"net/http"
	"os"
	"runtime/trace"
	"sync"
	"time"
)

// トークンを取得し、トークンを保存して、生成されたクライアントを返します。
func getClient(config *oauth2.Config) *http.Client {
	// ファイル token.json には、ユーザーのアクセスおよびリフレッシュトークンが保存されます。
	// これは、認証フローが初めて完了したときに自動的に作成されます。
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Webからトークンをリクエストし、取得したトークンを返します。
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("ブラウザで次のリンクにアクセスし、認証コードを入力してください: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("認証コードを読み取れませんでした: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Webからトークンを取得できませんでした: %v", err)
	}
	return tok
}

// ローカルファイルからトークンを取得します。
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// トークンをファイルパスに保存します。
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("資格情報ファイルを次の場所に保存しています: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("OAuthトークンをキャッシュできませんでした: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func listCourseWorkFromCourseId(srv *classroom.Service, courseId string, ctx context.Context, ch chan *classroom.CourseWork, wg *sync.WaitGroup) {
	defer trace.StartRegion(ctx, "list coursework from "+courseId).End()
	defer wg.Done()
	r, err := srv.Courses.CourseWork.List(courseId).Do()
	if err != nil {
		log.Fatalf("課題を取得できませんでした: %v", err)
	}
	if len(r.CourseWork) <= 0 {
		return
	}
	var wg2 sync.WaitGroup
	defer wg2.Wait()
	for _, c := range r.CourseWork {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			if isVisible, err := isCourseworkVisible(srv, c, ctx); isVisible && err == nil {
				ch <- c
			}
		}()
	}
}

func isCourseworkVisible(srv *classroom.Service, c *classroom.CourseWork, ctx context.Context) (bool, error) {
	defer trace.StartRegion(ctx, "work").End()
	var date string
	if c.DueDate != nil {
		date = fmt.Sprintf("%d-%02d-%02d", c.DueDate.Year, c.DueDate.Month, c.DueDate.Day)
	} else {
		currentDate := time.Now()
		date = currentDate.Format("2006-01-02")
	}
	// 日付をパースする
	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false, err
	}
	// 現在の日付を取得（時刻を無視して、日付部分だけを比較するために`Truncate`を使う）
	currentDate := time.Now().Truncate(24 * time.Hour)
	// 日付が今日より前かどうかを判定
	if parsedDate.Before(currentDate) {
		return false, nil
	}
	wr, err := srv.Courses.CourseWork.StudentSubmissions.List(c.CourseId, c.Id).Do()
	if err != nil {
		return false, err
	}
	for _, s := range wr.StudentSubmissions {
		if s.State == "TURNED_IN" {
			return false, nil
		}
	}
	return true, nil
}

func main() {
	f, err := os.Create("trace.out")
	if err != nil {
		log.Fatalln("Error:", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Fatalln("Error:", err)
		}
	}()

	if err := trace.Start(f); err != nil {
		log.Fatalln("Error:", err)
	}
	defer trace.Stop()

	_main()
}

func _main() {
	ctx2, task := trace.NewTask(context.Background(), "List course work")
	defer task.End()

	ctx := context.Background()
	b, err := os.ReadFile("client_secret.json")
	if err != nil {
		log.Fatalf("資格情報ファイルを読み取れませんでした: %v", err)
	}

	// これらのスコープを変更する場合、以前に保存した token.json を削除してください。
	config, err := google.ConfigFromJSON(b, classroom.ClassroomCoursesReadonlyScope, classroom.ClassroomCourseworkMeReadonlyScope)
	if err != nil {
		log.Fatalf("クライアントシークレットファイルを構成に解析できませんでした: %v", err)
	}
	client := getClient(config)

	srv, err := classroom.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Classroomクライアントを作成できませんでした: %v", err)
	}

	courseIds := []string{
		"681205615668", //5019データ通信工学Ⅰ
		"684704532100", //ソフトII（PYTHON)　【2年生】
		"675996740013", //5041ソフトウェア工学実習II-2
		"678236833659", //ソフトウエア工学実習Ⅱ-1
		"652602355337", //情報工学特別講座　R06
		"312535887497", //5043図形処理実習
		"660443542825", //5049基礎セミナー
		"672617426805", //5041データ通信実習Ⅰ
		"672617014173", //5046アプリケーション技術Ⅱ
		"604108757787", //情報システム設計Ⅱ2024
		"660396558271", //図形処理工学Ｉ【2年生】
	}
	ch := make(chan *classroom.CourseWork)
	var wg sync.WaitGroup

	for _, courseId := range courseIds {
		wg.Add(1) // ゴルーチンを追加
		go listCourseWorkFromCourseId(srv, courseId, ctx2, ch, &wg)
	}
	go func() {
		defer trace.StartRegion(ctx, "チャンネルクローズ").End()
		wg.Wait()
		close(ch) // ゴルーチンの終了後にチャネルを閉じる
	}()

	for c := range ch {
		fmt.Printf("%s (%s) link:%s\n", c.Title, c.Id, c.AlternateLink)
	}
}
