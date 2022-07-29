package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"

	"strings"

	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"

	"golang.org/x/net/html"
)

func getArticle(doc *html.Node) (*html.Node, error) {
	var body *html.Node
	var crawler func(*html.Node)
	crawler = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "article" {
			body = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			crawler(child)
		}
	}
	crawler(doc)
	if body != nil {

		return body, nil
	}
	return nil, errors.New("missing <article> in the node tree")
}

func renderNode(n *html.Node) string {
	var buf bytes.Buffer
	w := io.Writer(&buf)
	html.Render(w, n)
	return buf.String()
}

func hugo() {
	os.RemoveAll("export")
	os.RemoveAll("public")

	runhugo := exec.Command("hugo")
	err := runhugo.Run()

	if err != nil {
		log.Fatal(err)
	}
}

func visit(files *[]string) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		*files = append(*files, path)
		return nil
	}
}

func moveCss() {
	var files []string

	_, existerr := os.Stat("export-style/site-style.css")

	if existerr == nil {
		os.Remove("export-style/site-style.css")
	}

	walkerr := filepath.Walk("public", visit(&files))

	if walkerr != nil {
		log.Fatal(walkerr)
	}

	for _, file := range files {
		if strings.Contains(file, "book.min") {
			original, readerr := os.Open(file)

			if readerr != nil {
				log.Fatal(readerr)
			}

			new, writeerr := os.Create("export-style/site-style.css")

			if writeerr != nil {
				log.Fatal(writeerr)
			}

			_, copyerr := io.Copy(new, original)

			if copyerr != nil {
				log.Fatal(copyerr)
			}
		}
	}
}

func getDocDirectories() []string {
	var out []string

	bText, readerr := ioutil.ReadFile("public/index.html")

	if readerr != nil {
		log.Fatal(readerr)
	}

	text := string(bText)

	doc, _ := html.Parse(strings.NewReader(text))

	var crawler func(*html.Node)
	crawler = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" && (node.Parent.Data == "label" || node.Parent.Data == "li") {
			href := node.Attr[0].Val

			if strings.Contains(href, "docs") {
				url := strings.Split(href, "/docs/")[1]

				out = append(out, url)
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			crawler(child)
		}
	}
	crawler(doc)

	return out
}

func getPageClass(page *html.Node) string {
	pageStr := renderNode(page)

	pos := strings.LastIndex(pageStr, "book-page")
	pageStrBackHalf := pageStr[pos:]
	class := pageStrBackHalf[:strings.Index(pageStrBackHalf, "\"")]
	return class
}

func stripAnchors(pageStr string) string {
	page, err := html.Parse(strings.NewReader(pageStr))

	if err != nil {
		log.Fatal(err)
	}

	var crawler func(*html.Node)
	crawler = func(node *html.Node) {
		removed := false

		if node.Type == html.ElementNode && node.Data == "a" {
			for _, v := range node.Attr {
				if v.Key == "class" && v.Val == "anchor" {
					node.Parent.RemoveChild(node)
					removed = true
				}
			}
		}

		if !removed {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				crawler(child)
			}
		}
	}

	crawler(page)

	return renderNode(page)
}

func processDocument(md string, dist int) string {
	mdStripped := stripHugoTags(md)
	mdOut := addRootDistance(mdStripped, dist)
	return mdOut
}

func stripHugoTags(md string) string {
	// start by stripping headmatter and column dividers
	mdOut := strings.SplitN(md, "---", 3)[2]
	mdOut = strings.ReplaceAll(mdOut, "<--->", "")

	// grab every shortcode: bordered by {{< >}}
	// step one: make sure it looks like that as opposed to {{ < and > }}
	mdOut = strings.ReplaceAll(mdOut, "{{ <", "{{<")
	mdOut = strings.ReplaceAll(mdOut, "> }}", ">}}")

	// step two: grab start and end indices, get the substring of everything in between, and remove it
	// use ReplaceAll because odds are you're going to have the same one over and over if you have multiples
	for strings.Contains(mdOut, "{{<") {
		startIndex := strings.Index(mdOut, "{{<")
		endIndex := strings.Index(mdOut, ">}}") + 3
		between := mdOut[startIndex:endIndex]
		mdOut = strings.ReplaceAll(mdOut, between, "")
	}

	return mdOut
}

func addRootDistance(md string, dist int) string {
	/* adds distance to headers based on how far down it is
	 * the goal of this is to add header #'s onto things that aren't top-level or second-level
	 * so for example, if we have /game-directory/main-topic/sub-topic
	 * game-directory and main-topic have their h1's stay h1's
	 * while sub-topic has its h1's converted into h2's
	 * this makes headers populate in a more sensible manner for markdown
	 */

	// dist 0 is top-level, dist 1 is the first level down
	// these should operate as-is
	if dist == 0 || dist == 1 {
		return md
	}

	// documents are inconsistent re: lf vs crlf
	// check which one this is using first

	nl := "\n"

	if strings.Count(md, "\r\n") > 0 {
		nl = "\r\n"
	}

	// if dist = 3 (aka /foo-bar/foobar/foo/bar)
	// we want to turn ### Header into ##### Header
	// thus, using CRLF + # as basis to replace just the first # with the multiplied one
	newHeader := nl + strings.Repeat("#", dist)

	return strings.ReplaceAll(md, nl+"#", newHeader)
}

func main() {
	fmt.Println("Running Hugo...")
	hugo()
	fmt.Println("Hugo ran.")
	moveCss()

	dirs := getDocDirectories()

	type document struct {
		class     string
		name      string
		content   string
		nameMd    string
		contentMd string
	}

	var contents []document

	structure := `<html>
	<head></head>
	<body>
		<style>
		a.anchor {
			display: none;
		}
		</style>
		<main class="container flex">
			<div class="book-page markdown">
				md_placeholder
			</div>
		</main>
	</body>
	</html>`

	for _, s := range dirs {
		rootDist := len(strings.Split(s, "/")) - 2
		dirArray := strings.SplitN(s, "/", 2)

		fmt.Println(s)
		fmt.Println(rootDist)

		dirWriteHtml := "export/" + dirArray[0] + ".html"
		dirIndexHtml := "public/docs/" + dirArray[0] + "/index.html"
		dirReadHtml := "public/docs/" + dirArray[0] + "/" + dirArray[1] + "index.html"

		dirWriteMd := "export/" + dirArray[0] + ".md"
		dirReadMdDir := "content/docs/" + dirArray[0] + "/" + dirArray[1] + "_index.md"

		suffix := dirArray[1]

		if len(dirArray[1]) > 3 {
			suffix = suffix[:len(suffix)-1]
		}

		dirReadMdIndex := "content/docs/" + dirArray[0] + "/" + suffix + ".md"

		dirReadMdIndex = strings.Replace(dirReadMdIndex, "/.md", ".md", 1)

		inContents := false

		for _, v := range contents {
			if v.name == dirWriteHtml {
				inContents = true
			}
		}

		if !inContents {
			indexFile, indexerr := ioutil.ReadFile(dirIndexHtml)

			if indexerr != nil {
				log.Fatal(indexerr)
			}

			index, _ := html.Parse(strings.NewReader(string(indexFile)))

			class := getPageClass(index)

			newdoc := document{class: class, name: dirWriteHtml, content: "", nameMd: dirWriteMd, contentMd: ""}
			contents = append(contents, newdoc)
		}

		bText, readerr := ioutil.ReadFile(dirReadHtml)

		if readerr != nil {
			log.Fatal(readerr)
		}

		text := string(bText)

		doc, _ := html.Parse(strings.NewReader(text))

		an, readerr2 := getArticle(doc)

		if readerr2 != nil {
			log.Fatal(readerr2)
		}

		mdout, mddir := "", ""

		if _, err := os.Stat(dirReadMdIndex); err == nil {
			mddir = dirReadMdIndex
		} else if _, err := os.Stat(dirReadMdDir); err == nil {
			mddir = dirReadMdDir
		} else {
			println("Neither " + dirReadMdIndex + " or " + dirReadMdDir)
		}

		if mddir != "" {
			bMd, mderr := ioutil.ReadFile(mddir)

			if mderr != nil {
				log.Fatal(mderr)
			}

			mdout = string(bMd)
		}

		for i, v := range contents {
			if v.name == dirWriteHtml {
				contents[i].content += renderNode(an)
				contents[i].contentMd += processDocument(mdout, rootDist)
			}
		}
	}

	exportmkdirerr := os.Mkdir("export", 0644)
	if exportmkdirerr != nil {
		fmt.Println(exportmkdirerr)
	}

	exportmkdirfinalerr := os.Mkdir("export/export-final", 0644)
	if exportmkdirfinalerr != nil {
		fmt.Println(exportmkdirerr)
	}

	for _, v := range contents {
		noArticleContent := strings.ReplaceAll(v.content, "<article class=\"markdown\">", "")
		noArticleContent = strings.ReplaceAll(noArticleContent, "</article>", "")

		contentOut := strings.Replace(structure, "md_placeholder", noArticleContent, 1)
		contentOut = strings.Replace(contentOut, "book-page", v.class, 1)
		contentOut = stripAnchors(contentOut)

		mdContentOut := v.contentMd

		if _, err := os.Stat(v.name); errors.Is(err, os.ErrNotExist) {
			os.WriteFile(v.name, []byte(contentOut), 0644)
		}

		if _, err := os.Stat(v.nameMd); errors.Is(err, os.ErrNotExist) {
			os.WriteFile(v.nameMd, []byte(mdContentOut), 0644)
		}

		pandocBase := strings.Replace(strings.Split(v.name, ".")[0], "export", "export/export-final", 1)
		fmt.Println(pandocBase)
		pandocHtmlOut := pandocBase + "-processed.html"
		pandocEpubOut := pandocBase + ".epub"

		pandocHtml := exec.Command("pandoc", v.name,
			"-c", "export-style/site-style.css",
			"--self-contained",
			"-f", "html",
			"-t", "html",
			"-s", "-o",
			pandocHtmlOut)

		pandocHtmlErr := pandocHtml.Run()

		if pandocHtmlErr != nil {
			fmt.Println("Error creating processed html: " + pandocHtmlOut)
			fmt.Println(pandocHtmlErr)
		}

		pandocMd := exec.Command("pandoc", v.nameMd,
			"-c", "export-style/epub-style.css",
			"-f", "markdown",
			"-t", "epub",
			"-s", "-o",
			pandocEpubOut)

		pandocMdErr := pandocMd.Run()

		if pandocMdErr != nil {
			fmt.Println("Error creating epub: " + pandocEpubOut)
			fmt.Println(pandocMdErr)
		}
	}
}
