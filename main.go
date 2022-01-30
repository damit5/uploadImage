package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/EDDYCJY/gsema"
	"github.com/json-iterator/go"
	"golang.org/x/net/publicsuffix"
	"io"
	"io/ioutil"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var client http.Client
var isCover bool // 是否覆盖原来的文件
var semaphore *gsema.Semaphore
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}


/*
获取微信公众号token
 */
func getAccessToken(appid string, secret string) string {
	target := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s", appid, secret)
	resp, _ := client.Get(target)
	res, _ := ioutil.ReadAll(resp.Body)
	if jsoniter.Get(res, "expires_in").ToString() == "7200" {
		return jsoniter.Get(res, "access_token").ToString()
	} else {
		fmt.Println(string(res))
		os.Exit(0)
	}
	return ""
}

/*
构造上传请求，上传图片，获取上传图片的地址
*/
func uploadTempImage(accessToken string, imgFilePath string) string {
	// 上传网址
	target := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/media/upload?access_token=%s&type=image", accessToken)
	// 要上传的文件
	file, _ := os.Open(imgFilePath)
	defer file.Close()

	// 设置body数据并写入缓冲区
	bodyBuff := bytes.NewBufferString("") //bodyBuff := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuff)
	_ = bodyWriter.SetBoundary(fmt.Sprintf("-----------------------------%d", rand.Int()))
	// 加入图片二进制
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes("source"), escapeQuotes(filepath.Base(file.Name()))))
	h.Set("Content-Type", "image/png")
	part, _ := bodyWriter.CreatePart(h)
	_, _ = io.Copy(part, file)

	// 自动补充boundary结尾
	_ = bodyWriter.Close()


	//创建请求
	req, _ := http.NewRequest("POST", target, bodyBuff)
	req.ContentLength = int64(bodyBuff.Len())
	req.Header.Set("Content-Type", bodyWriter.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; Win64; x64; rv:89.0) Gecko/20100101 Firefox/89.0")

	resp, _ := client.Do(req)
	res, _ := ioutil.ReadAll(resp.Body)
	mediaId := jsoniter.Get(res, "media_id").ToString()
	imageUrl := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/media/get?access_token=%s&media_id=%s", accessToken, mediaId)

	return imageUrl
}

/*
提取markdown中的图片地址，返回两个结果
1. 文件中的路径，比如 a.assets/aaa.png ==> 方便后面替换
2. 处理后的路径，比如 /User/d4m1ts/markdown/a.assets/aaa.png ==> 方便后面上传
 */
func extractMdImage(filePath string) ([]string, []string) {
	// 当前文件路径
	basePath := strings.Join(strings.Split(filePath, "/")[:len(strings.Split(filePath, "/"))-1], "/")
	var rawImageRes []string
	var absImageRes []string
	mdImageRegex, _ := regexp.Compile("!\\[.*?\\]\\((.*?)\\)")
	res, _ := ioutil.ReadFile(filePath)
	source := string(res)
	result := mdImageRegex.FindAllStringSubmatch(source, -1)
	for _,i := range result {
		rawImageRes = append(rawImageRes, i[1])
		absImageRes = append(absImageRes, basePath +"/" +i[1])
	}
	return rawImageRes, absImageRes
}

/*
修改markdown中上传图片的地址
 */
func replaceMdImage(filePath string, oldImages []string, newImages []string){
	if len(oldImages) == len(newImages) {
		res, _ := ioutil.ReadFile(filePath)
		source := string(res)
		for i:=0;i<len(oldImages);i++ {
			source = strings.Replace(source, oldImages[i], newImages[i], -1)
		}
		if isCover {
			_ = ioutil.WriteFile(filePath, []byte(source), 0666)
			fmt.Println("覆盖文件：" + filePath)
		} else {
			_ = ioutil.WriteFile(filePath+".txt", []byte(source), 0666)
			fmt.Println("保存文件：" + filePath + ".txt")
		}

	} else {
		panic("replaceMdImage 图片长度不对等！！！")
	}
}

/*
对每一个文件单独进行处理，抽象到一个函数中
 */
func oneFileMain(accessToken string, path string){
	// 解析文件中的图片
	rawImageRes, absImageRes := extractMdImage(path)
	var newImages []string
	for num, ai := range absImageRes {
		fmt.Println(fmt.Sprintf("共%d张图片，正在上传第 %d 张图片", len(absImageRes), num+1))
		image := uploadTempImage(accessToken, ai)
		newImages = append(newImages, image)
	}
	// 替换图片
	replaceMdImage(path, rawImageRes, newImages)
	defer semaphore.Done()
}


func main() {
	var filePath string
	var dirPath string
	var proxyUrl string
	var appid string
	var secret string
	var t int

	flag.Usage = func() {
		fmt.Println(`
       ____ __           _____      
  ____/ / // / ____ ___ <  / /______
 / __  / // /_/ __  __ \/ / __/ ___/
/ /_/ /__  __/ / / / / / / /_(__  )
\__,_/  /_/ /_/ /_/ /_/_/\__/____/
			`)
		fmt.Fprintf(flag.CommandLine.Output(), "markdown图片自动上传到图床\n\nUsage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.StringVar(&filePath, "f", "", "单个markdown文件路径")
	flag.StringVar(&dirPath, "d", "", "整个markdown文件夹路径")
	flag.BoolVar(&isCover, "cover", false, "是否覆盖源文件，默认不覆盖")
	flag.StringVar(&proxyUrl, "p", "", "使用代理，如socks5://127.0.0.1:1080")
	flag.StringVar(&appid, "appid", "", "微信公众号appid")
	flag.StringVar(&secret, "secret", "", "微信公众号secret")
	flag.IntVar(&t, "t", 3, "线程数量，仅会在多文件时使用")
	flag.Parse()
	if flag.NFlag() == 0 { // 使用的命令行参数个数
		flag.Usage()
		os.Exit(0)
	}
	// 初始化Client
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if proxyUrl != "" {
		proxy,_ := url.Parse(proxyUrl)
		netTransport := &http.Transport{
			Proxy:                 http.ProxyURL(proxy),
			MaxIdleConnsPerHost:   10,
			ResponseHeaderTimeout: time.Second * time.Duration(5),
		}
		client = http.Client{
			Transport: netTransport,
			Jar:     jar,
			Timeout: time.Second * 10,
		}
	} else {
		client = http.Client{
			Jar:     jar,
			Timeout: time.Second * 10,
		}
	}

	// 获取token，可以多次用，所以第一次获取即可
	accessToken := getAccessToken(appid, secret)
	semaphore = gsema.NewSemaphore(t)

	if flag.Lookup("f").Value.String() != "" { // 单个markdown
		semaphore.Add(1)
		oneFileMain(accessToken, filePath)
	} else if flag.Lookup("d").Value.String() != "" { // 文件夹
		cmd := exec.Command("bash", "-c", fmt.Sprintf("find %s -name *.md", dirPath))
		res, _ := cmd.CombinedOutput()
		mdFiles := strings.Split(string(res), "\n")
		if mdFiles[len(mdFiles)-1] == "" {	// 最后一个是""就删除
			mdFiles = mdFiles[:len(mdFiles)-1]
		}
		for _,fp := range mdFiles {
			semaphore.Add(1)
			go oneFileMain(accessToken, fp)
		}
	} else {
		flag.Usage()
	}
	semaphore.Wait()
}
