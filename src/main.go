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
const krokiUrl string = "https://kroki.io/plantuml/"

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
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusInternalServerError, Body: "Internal Server Error"}, nil
	}

	fileBytes, err := fetchPlantUmlImage(*plantUmlText, filetype)
	if err != nil {
		fmt.Println(err)
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusInternalServerError, Body: "Internal Server Error"}, nil
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
	plantumlBase64, err := encode(plantUmlText)
	if err != nil {
		return nil, fmt.Errorf("fail on encode(plantUmlText): %w", err)
	}

	url, _ := url.Parse(krokiUrl)
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

func encode(input string) (string, error) {
	var buffer bytes.Buffer
	writer, err := zlib.NewWriterLevel(&buffer, 9)
	if err != nil {
		return "", fmt.Errorf("fail to create the writer: %w", err)
	}
	_, err = writer.Write([]byte(input))
	writer.Close()
	if err != nil {
		return "", fmt.Errorf("fail to create the payload: %w", err)
	}
	result := base64.URLEncoding.EncodeToString(buffer.Bytes())
	return result, nil
}
