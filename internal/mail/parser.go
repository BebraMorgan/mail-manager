package mail

import (
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/emersion/go-message/mail"

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

				doc.Find(
					"script, style, noscript",
				).Remove()

				htmlText = doc.Find("body").Text()
				if htmlText == "" {
					htmlText = doc.Text()
				}
			}
		}
	}

	if strings.TrimSpace(plainText) != "" {
		return strings.TrimSpace(plainText), nil
	}

	return strings.TrimSpace(htmlText), nil
}
