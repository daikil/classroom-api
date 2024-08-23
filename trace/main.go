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
	defer trace.StartRegion(ctx, "listCourseWork").End()
	defer wg.Done()
	r, err := srv.Courses.CourseWork.List(courseId).Do()
	if err != nil {
		log.Fatalf("課題を取得できませんでした: %v", err)
	}
	var wg2 sync.WaitGroup
	for _, coursework := range r.CourseWork {
		wg2.Add(1)
		go func(c *classroom.CourseWork) {
			defer wg2.Done()
			if visible, err := isCourseworkVisible(srv, c, ctx); visible && err == nil {
				ch <- c
			}
		}(coursework)
	}
	wg2.Wait()
}

func isCourseworkVisible(srv *classroom.Service, c *classroom.CourseWork, ctx context.Context) (bool, error) {
	defer trace.StartRegion(ctx, "checkVisibility").End()
	var date string
	if c.DueDate != nil {
		date = fmt.Sprintf("%d-%02d-%02d", c.DueDate.Year, c.DueDate.Month, c.DueDate.Day)
	} else {
		currentDate := time.Now()
		date = currentDate.Format("2006-01-02")
	}
	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false, err
	}
	currentDate := time.Now().Truncate(24 * time.Hour)
	if parsedDate.Before(currentDate) {
		return false, nil
	}
	submissions, err := srv.Courses.CourseWork.StudentSubmissions.List(c.CourseId, c.Id).Do()
	if err != nil {
		return false, err
	}
	//課題の提出状況を確認して、提出済みであれば表示しない
	for _, submission := range submissions.StudentSubmissions {
		if submission.State == "TURNED_IN" {
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
		///
	}
	ch := make(chan *classroom.CourseWork)
	var wg sync.WaitGroup

	for _, courseId := range courseIds {
		wg.Add(1) // ゴルーチンを追加
		go listCourseWorkFromCourseId(srv, courseId, ctx2, ch, &wg)
	}
	go func() {
		wg.Wait()
		close(ch) // ゴルーチンの終了後にチャネルを閉じる
	}()

	for coursework := range ch {
		fmt.Printf("%s (%s) link:%s\n", coursework.Title, coursework.Id, coursework.AlternateLink)
	}
}
