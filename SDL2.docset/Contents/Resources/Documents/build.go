package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/cheggaaa/pb.v1"

	"database/sql"

	"github.com/PuerkitoBio/goquery"
	_ "github.com/mattn/go-sqlite3"
)

const (
	BASE = "https://wiki.libsdl.org"
)

var (
	_wait uint
)

func init() {
	flag.UintVar(&_wait, "w", 5,
		"wait between requests (surge protection is harsh)")
	flag.Parse()
}

func downloadSubpage(db *sql.DB, index int, cat, name, link string) error {
	u, err := url.Parse(BASE + link)
	if err != nil {
		return err
	}
	doc, err := goquery.NewDocument(BASE + link)
	if err != nil {
		return err
	}
	page := doc.Find("#page")
	if page.Length() == 0 {
		return errors.New("no #page in html, surge protection active maybe?")
	}
	var dir string
	if cat == "Category" {
		dir = cat
	} else {
		dir = map[string]string{
			"Hints":        "Constant",
			"Enumerations": "Enum",
			"Structures":   "Struct",
			"Functions":    "Function",
		}[cat]
	}
	file := u.Path[1:] + ".html"
	path := filepath.Join(dir, file)
	fd, err := os.OpenFile(path,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0750)
	if err != nil {
		return err
	}
	lhr := page.Find("hr").Last()
	lhr.NextAll().Remove()
	lhr.Remove()
	html, err := page.Html()
	if err != nil {
		fd.Close()
		return err
	}
	fmt.Fprintf(fd, `<!DOCTYPE html><html>
		<head><title>%s</title></head>
		<body>%s</body>
		</html>`, name, html)
	fd.Close()
	db.Exec(
		"insert into searchIndex (id, name, type, path) values (?, ?, ?, ?)",
		index, name, dir, dir+"/"+file)
	return nil
}

func removeContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	dirs := []string{"Category", "Constant", "Enum", "Struct", "Function"}
	for _, dir := range dirs {
		err := removeContents(dir)
		if err != nil {
			log.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite3", "../docSet.dsidx")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.Exec("delete from searchIndex")
	index := 0
	{
		doc, err := goquery.NewDocument(BASE + "/CategoryAPI")
		if err != nil {
			log.Fatal(err)
		}
		page := doc.Find("#page")
		sr := page.Find(".searchresults")
		for i, h2 := range page.Find("h2").Nodes {
			cat := h2.FirstChild.Data
			pages := page.FindNodes(sr.Get(i)).Find("a").Nodes
			fmt.Println("getting", cat)
			bar := pb.New(len(pages))
			bar.ManualUpdate = true
			bar.Start()
			for _, a := range pages {
				err := downloadSubpage(db, index, cat, a.FirstChild.Data,
					a.Attr[0].Val)
				if err != nil {
					log.Fatal(err)
				}
				time.Sleep(time.Second * time.Duration(_wait))
				index++
				bar.Increment()
				bar.Update()
			}
			bar.Finish()
		}
	}
	{
		fmt.Println("getting categories")
		doc, err := goquery.NewDocument(BASE + "/APIByCategory")
		if err != nil {
			log.Fatal(err)
		}
		page := doc.Find("#page")
		{
			h, err := page.Html()
			if err != nil {
				log.Fatal(err)
			}
			fd, err := os.OpenFile("index.html",
				os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0750)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Fprintf(fd, `<!DOCTYPE html><html>
<head><title>SDL 2.0 API</title></head>
<body>%s</body>
</html>`, h)
			fd.Close()
		}
		pages := page.Find("td a").Nodes
		bar := pb.New(len(pages))
		bar.ManualUpdate = true
		bar.Start()
		for _, a := range pages {
			if !strings.HasPrefix(a.Attr[0].Val, "/") {
				continue
			}
			err := downloadSubpage(db, index, "Category",
				a.FirstChild.Data, a.Attr[0].Val)
			if err != nil {
				log.Fatal(err)
			}
			time.Sleep(time.Second * time.Duration(_wait))
			index++
			bar.Increment()
			bar.Update()
		}
		bar.Finish()
	}
	{
		fmt.Println("fixing links")
		files := make([]string, 0, 100)
		files = append(files, "index.html")
		for _, dir := range dirs {
			df, err := filepath.Glob(filepath.Join(dir, "*.html"))
			if err != nil {
				log.Fatal(err)
			}
			files = append(files, df...)
		}
		bar := pb.New(len(files))
		bar.ManualUpdate = true
		bar.Start()
		for _, file := range files {
			data, err := ioutil.ReadFile(file)
			if err != nil {
				log.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
			if err != nil {
				log.Fatal(err)
			}
			changed := false
			doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
				href := s.AttrOr("href", "")
				u, err := url.Parse(href)
				if err != nil {
					log.Fatal(err)
				}
				var row *sql.Row
				if strings.HasPrefix(u.Path, "/SDL_") {
					row = db.QueryRow(
						"select path from searchIndex where name = ?",
						u.Path[1:])
				} else if strings.HasPrefix(u.Path, "/Category") {
					row = db.QueryRow(
						"select path from searchIndex where path = ?",
						"Category"+u.Path+".html")
				} else {
					return
				}
				var path string
				if err := row.Scan(&path); err != nil {
					return
				}
				nhref := path + "#" + u.Fragment
				if file != "index.html" {
					nhref = "../" + nhref
				}
				s.SetAttr("href", nhref)
				changed = true
			})
			if changed {
				h, err := doc.Html()
				if err != nil {
					log.Fatal(err)
				}
				ioutil.WriteFile(file, []byte(h), 0750)
			}
			bar.Increment()
			bar.Update()
		}
		bar.Finish()
	}
}
