package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

const notionApiUrl string = "https://api.notion.com/v1/blocks/"
const notionVersion string = "2022-02-22"
const serverUrl string = "https://www.plantuml.com/plantuml/"

var notionAccessKey = os.Getenv("NOTION_ACCESS_KEY")

func handler(request events.APIGatewayProxyRequest) (*events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod != http.MethodGet {
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: "Bad Request"}, nil
	}

	filetype, ok := request.QueryStringParameters["filetype"]
	if !ok || !map[string]bool{"png": true, "svg": true}[filetype] {
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: "Bad Request"}, nil
	}
	blockId, ok := request.QueryStringParameters["blockId"]
	if !ok {
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: "Bad Request"}, nil
	}
	plantUmlText, err := fetchBlockText(blockId)
	if err != nil {
		fmt.Println(err)
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusInternalServerError, Body: "Fail to fetch text block from Notion.\nMake sure that Notion connect is set up on the page you want to embed."}, nil
	}

	fileBytes, err := fetchPlantUmlImage(*plantUmlText, filetype)
	if err != nil {
		fmt.Println(err)
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusInternalServerError, Body: "Fail to generate image. Ask admin."}, nil
	}

	res := &events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Cache-Control":               "no-store, no-cache, must-revalidate, max-age=0",
			"Access-Control-Allow-Origin": "https://www.notion.so",
		},
	}
	switch filetype {
	case "png":
		body := base64.StdEncoding.EncodeToString(fileBytes)
		res.Headers["Content-Type"] = "image/png"
		res.Body = body
		res.IsBase64Encoded = true
		break
	case "svg":
		res.Headers["Content-Type"] = "image/svg+xml"
		res.Body = string(fileBytes)
		res.IsBase64Encoded = false
		break
	}
	return res, nil
}

func main() {
	lambda.Start(handler)
}

func fetchBlockText(blockId string) (*string, error) {
	url, _ := url.Parse(notionApiUrl)
	url.Path = path.Join(url.Path, blockId)
	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("fail to create http.Request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+notionAccessKey)
	req.Header.Add("Notion-Version", notionVersion)
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fail on client.Do(req): %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status: got %v, expected %v", res.StatusCode, http.StatusOK)
	}

	var jsonMap map[string]interface{}
	json.NewDecoder(res.Body).Decode(&jsonMap)
	plantumlText := jsonMap["code"].(map[string]interface{})["rich_text"].([]interface{})[0].(map[string]interface{})["text"].(map[string]interface{})["content"].(string)
	return &plantumlText, nil
}

func fetchPlantUmlImage(plantUmlText, filetype string) ([]byte, error) {
	plantumlBase64, err := plantUMLEncode(plantUmlText)
	if err != nil {
		return nil, fmt.Errorf("fail on plantUMLEncode(plantUmlText): %w", err)
	}

	url, _ := url.Parse(serverUrl)
	url.Path = path.Join(url.Path, filetype, plantumlBase64)
	res, err := http.Get(url.String())
	if err != nil {
		return nil, fmt.Errorf("fail on http.Get(): %w", err)
	}
	defer res.Body.Close()

	fileBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("fail on ioutil.ReadAll(res.Body): %w", err)
	}
	return fileBytes, nil
}

// エンコードに使うPlantUML独自のアルファベット
var encodeMap = []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-_")

// PlantUMLEncode はPlantUML用にテキストをエンコードします
func plantUMLEncode(input string) (string, error) {
	// Step 1: zlib圧縮
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, err := w.Write([]byte(input))
	if err != nil {
		return "", err
	}
	w.Close()

	compressed := buf.Bytes()

	// Step 2: 独自Base64エンコード
	return encode64(compressed), nil
}

// encode64 はPlantUML独自Base64エンコードを行う
func encode64(data []byte) string {
	var result []byte

	for i := 0; i < len(data); i += 3 {
		if i+2 < len(data) {
			n := uint(data[i])<<16 | uint(data[i+1])<<8 | uint(data[i+2])
			result = append(result, encode3bytes(n)...) // スライスを展開する
		} else if i+1 < len(data) {
			n := uint(data[i])<<16 | uint(data[i+1])<<8
			result = append(result, encode3bytes(n)[:3]...) // 2バイト分だけ使う
		} else {
			n := uint(data[i]) << 16
			result = append(result, encode3bytes(n)[:2]...) // 1バイト分だけ使う
		}
	}
	return string(result)
}

// 3バイトを4文字にエンコード
func encode3bytes(n uint) []byte {
	return []byte{
		encodeMap[(n>>18)&0x3F],
		encodeMap[(n>>12)&0x3F],
		encodeMap[(n>>6)&0x3F],
		encodeMap[n&0x3F],
	}
}
