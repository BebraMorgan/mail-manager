package mail

import (
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/emersion/go-message/mail"
	"golang.org/x/net/html"

	_ "github.com/emersion/go-message/charset"
)

func ParseBody(r io.Reader) (string, error) {
	reader, err := mail.CreateReader(r)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var plainText string
	var htmlText string

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		header, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}

		contentType, _, err := header.ContentType()
		if err != nil {
			return "", err
		}

		data, err := io.ReadAll(io.LimitReader(
			part.Body, 1024*1024+1,
		))
		if err != nil {
			return "", err
		}
		if len(data) > 1024*1024 {
			return "", io.ErrShortBuffer
		}

		switch strings.ToLower(contentType) {
		case "text/plain":
			if plainText == "" {
				plainText = string(data)
			}

		case "text/html":
			if htmlText == "" {
				doc, err := goquery.NewDocumentFromReader(
					strings.NewReader(string(data)),
				)
				if err != nil {
					continue
				}

				doc.Find("script, style, noscript, blockquote, .gmail_quote, .yahoo_quoted, .moz-cite-prefix").Remove()

				var b strings.Builder
				for _, node := range doc.Nodes {
					htmlTextNode(node, &b)
				}
				htmlText = NormalizeBody(b.String())
			}
		}
	}

	if strings.TrimSpace(plainText) != "" {
		return strings.TrimSpace(plainText), nil
	}

	return strings.TrimSpace(htmlText), nil
}

func htmlTextNode(n *html.Node, b *strings.Builder) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "script", "style", "noscript", "head":
			return
		}
	}
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
		b.WriteByte(' ')
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		htmlTextNode(child, b)
	}
	if n.Type == html.ElementNode {
		switch n.Data {
		case "p", "div", "br", "li", "tr", "h1", "h2", "h3", "section":
			b.WriteByte('\n')
		}
	}
}
