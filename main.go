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
上传到sm.ms
 */
func uploadImageToSmms(token string, imgFilePath string) string {
	// 上传网址
	target := "https://sm.ms/api/v2/upload"
	// 要上传的文件
	file, _ := os.Open(imgFilePath)
	defer file.Close()
	if file == nil || strings.HasSuffix(imgFilePath, "/"){
		fmt.Println(fmt.Sprintf("%s is nil", imgFilePath))
		return ""
	}

	// 设置body数据并写入缓冲区
	bodyBuff := bytes.NewBufferString("") //bodyBuff := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuff)
	_ = bodyWriter.SetBoundary(fmt.Sprintf("-----------------------------%d", rand.Int()))
	// 加入图片二进制
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes("smfile"), escapeQuotes(filepath.Base(file.Name()))))
	h.Set("Content-Type", "image/png")
	part, _ := bodyWriter.CreatePart(h)
	_, _ = io.Copy(part, file)

	_ = bodyWriter.WriteField("format", "json")

	// 自动补充boundary结尾
	_ = bodyWriter.Close()

	//创建请求
	req, _ := http.NewRequest("POST", target, bodyBuff)
	req.ContentLength = int64(bodyBuff.Len())
	req.Header.Set("Content-Type", bodyWriter.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; Win64; x64; rv:89.0) Gecko/20100101 Firefox/89.0")
	req.Header.Set("Authorization", token)

	var imageUrl string

	resp, err_client := client.Do(req)
	if resp != nil || err_client == nil {
		res, err_ioutil := ioutil.ReadAll(resp.Body)

		if err_ioutil == nil {
			//fmt.Println(string(res))
			if jsoniter.Get(res, "code").ToString() == "flood" {
				time.Sleep(time.Second * 60)
				return uploadImageToSmms(token, imgFilePath)
			} else if jsoniter.Get(res, "data").Get("url").ToString() != "" {
				imageUrl = jsoniter.Get(res, "data").Get("url").ToString()
			} else if jsoniter.Get(res, "images").ToString() != "" {
				imageUrl = jsoniter.Get(res, "images").ToString()
			}
		} else {
			fmt.Println(string(res))
			imageUrl = ""
		}
	} else {
		fmt.Println(err_client)
	}

	if imageUrl == "" {
		fmt.Println(imgFilePath + "\timageUrl: " + imageUrl)
	}
	return imageUrl
}

/*
上传图片到语雀
uploadUrl: 上传的URL，https://www.yuque.com/api/upload/attach?attachable_type=Doc&attachable_id=aaaaa&type=image&ctoken=xxxxx

 */
func uploadImagetoYuque(uploadUrl string, cookie string, imgFilePath string) string {
	// 要上传的文件
	file, _ := os.Open(imgFilePath)
	defer file.Close()
	if file == nil || strings.HasSuffix(imgFilePath, "/"){
		fmt.Println(fmt.Sprintf("%s is nil", imgFilePath))
		return ""
	}

	// 设置body数据并写入缓冲区
	bodyBuff := bytes.NewBufferString("") //bodyBuff := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuff)
	_ = bodyWriter.SetBoundary(fmt.Sprintf("-----------------------------%d", rand.Int()))
	// 加入图片二进制
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes("file"), escapeQuotes(filepath.Base(file.Name()))))
	h.Set("Content-Type", "image/png")
	part, _ := bodyWriter.CreatePart(h)
	_, _ = io.Copy(part, file)

	// 自动补充boundary结尾
	_ = bodyWriter.Close()

	req, _ := http.NewRequest("POST", uploadUrl, bodyBuff)
	req.ContentLength = int64(bodyBuff.Len())
	req.Header.Set("Content-Type", bodyWriter.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; Win64; x64; rv:89.0) Gecko/20100101 Firefox/89.0")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Referer", "https://www.yuque.com/da-labs/secnotes/syhdn5/edit?toc_node_uuid=oJ_aaaaaaaaaaaaa")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
	}  else {
		res, resperr := ioutil.ReadAll(resp.Body)
		if resperr != nil {
			fmt.Println(resperr)
		} else {
			imageUrl := jsoniter.Get(res, "data").Get("url").ToString()
			return imageUrl
		}

	}

	return ""
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
		if !strings.HasPrefix(i[1], "http") && i[1] != ""{ // 避免网络图片和空路径图片
			if strings.Count(i[1], "%") > 5 {
				uni, _ := url.QueryUnescape(i[1])
				rawImageRes = append(rawImageRes, i[1])
				absImageRes = append(absImageRes, basePath +"/" + uni)
			} else {
				rawImageRes = append(rawImageRes, i[1])
				absImageRes = append(absImageRes, basePath +"/" + i[1])
			}
		}
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
			// 避免速度太快被封禁了，然后全部替换成了空了
			if newImages[i] == "" {
				fmt.Printf("上传 '%s' 时疑似IP被封禁，%s 图片为空\n", filePath, oldImages[i])
				continue
			}
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
func oneFileMain(path string, sign string, tokens ...string){
	// 解析文件中的图片
	rawImageRes, absImageRes := extractMdImage(path)
	var newImages []string
	for num, ai := range absImageRes {
		fmt.Println(fmt.Sprintf("共%d张图片，正在上传第 %d 张图片", len(absImageRes), num+1))
		var image string
		if sign == "wx" {
			image = uploadTempImage(tokens[0], ai)
		} else if sign == "sm" {
			image = uploadImageToSmms(tokens[0], ai)
		} else if sign == "yq" {
			image = uploadImagetoYuque(tokens[0], tokens[1], ai)
		}
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
	var wxappid string
	var wxsecret string
	var smtoken string
	var t int
	var accessToken string
	var yuqueUrl string
	var yuqueCookie string

	flag.Usage = func() {
		fmt.Println(`
       ____ __           _____      
  ____/ / // / ____ ___ <  / /______
 / __  / // /_/ __  __ \/ / __/ ___/
/ /_/ /__  __/ / / / / / / /_(__  )
\__,_/  /_/ /_/ /_/ /_/_/\__/____/
			`)
		fmt.Println("Usage: uploadImage -f xxx.md -wxsecret xxx -wxappid xxx")
		fmt.Println("Usage: uploadImage -f xxx.md -smtoken xxx")
		fmt.Println("Usage: uploadImage -f xxx.md -yuqueurl http://xxx -yuquecookie xxx\n")
		fmt.Fprintf(flag.CommandLine.Output(), "markdown图片自动上传到图床\n\nUsage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.StringVar(&filePath, "f", "", "单个markdown文件路径")
	flag.StringVar(&dirPath, "d", "", "整个markdown文件夹路径")
	flag.BoolVar(&isCover, "cover", false, "是否覆盖源文件，默认不覆盖")
	flag.StringVar(&proxyUrl, "p", "", "使用代理，如socks5://127.0.0.1:1080")
	flag.StringVar(&wxappid, "wxappid", "", "微信公众号appid")
	flag.StringVar(&wxsecret, "wxsecret", "", "微信公众号secret")
	flag.IntVar(&t, "t", 3, "线程数量，仅会在多文件时使用")
	flag.StringVar(&smtoken, "smtoken", "", "sm.ms的token")
	flag.StringVar(&yuqueUrl, "yuqueurl", "", "语雀上传的URL")
	flag.StringVar(&yuqueCookie, "yuquecookie", "", "语雀上传的Cookie")
	flag.Parse()
	if flag.NFlag() == 0 { // 使用的命令行参数个数
		flag.Usage()
		os.Exit(0)
	}
	// 初始化Client
	timeout := time.Second * 60
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if proxyUrl != "" {
		proxy,_ := url.Parse(proxyUrl)
		netTransport := &http.Transport{
			Proxy:                 http.ProxyURL(proxy),
			MaxIdleConnsPerHost:   60,
			ResponseHeaderTimeout: time.Second * time.Duration(60),
		}
		client = http.Client{
			Transport: netTransport,
			Jar:     jar,
			Timeout: timeout,
		}
	} else {
		client = http.Client{
			Jar:     jar,
			Timeout: timeout,
		}
	}

	// 获取wx token，可以多次用，所以第一次获取即可
	if flag.Lookup("wxappid").Value.String() != "" && flag.Lookup("wxsecret").Value.String() != ""{
		accessToken = getAccessToken(wxappid, wxsecret)
	}
	semaphore = gsema.NewSemaphore(t)

	if flag.Lookup("f").Value.String() != "" { // 单个markdown上传
		semaphore.Add(1)
		if flag.Lookup("wxappid").Value.String() != "" && flag.Lookup("wxsecret").Value.String() != "" {
			oneFileMain(filePath, "wx", accessToken)
		} else if flag.Lookup("smtoken").Value.String() != "" {
			oneFileMain(filePath, "sm", smtoken)
		} else if flag.Lookup("yuqueurl").Value.String() != "" && flag.Lookup("yuquecookie").Value.String() != "" {
			oneFileMain(filePath, "yq", yuqueUrl, yuqueCookie)
		}
	} else if flag.Lookup("d").Value.String() != "" { // 文件夹
		cmd := exec.Command("bash", "-c", fmt.Sprintf("find %s -name \\*.md", dirPath))
		res, _ := cmd.CombinedOutput()
		mdFiles := strings.Split(string(res), "\n")
		if mdFiles[len(mdFiles)-1] == "" {	// 最后一个是""就删除
			mdFiles = mdFiles[:len(mdFiles)-1]
		}
		for _,fp := range mdFiles {
			semaphore.Add(1)
			if flag.Lookup("wxappid").Value.String() != "" && flag.Lookup("wxsecret").Value.String() != "" {
				go oneFileMain(fp, "wx", accessToken)
			} else if flag.Lookup("smtoken").Value.String() != "" {
				go oneFileMain(fp, "sm", smtoken)
			} else if flag.Lookup("yuqueurl").Value.String() != "" && flag.Lookup("yuquecookie").Value.String() != "" {
				go oneFileMain(fp, "yq", yuqueUrl, yuqueCookie)
			}
		}
	} else {
		flag.Usage()
	}
	semaphore.Wait()
}
