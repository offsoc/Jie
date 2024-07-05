package mode

import (
    "context"
    "encoding/json"
    "fmt"
    "github.com/panjf2000/ants/v2"
    "github.com/projectdiscovery/katana/pkg/output"
    "github.com/thoas/go-funk"
    "github.com/yhy0/Jie/conf"
    "github.com/yhy0/Jie/crawler"
    "github.com/yhy0/Jie/crawler/crawlergo"
    "github.com/yhy0/Jie/crawler/crawlergo/config"
    "github.com/yhy0/Jie/crawler/crawlergo/model"
    "github.com/yhy0/Jie/fingprints"
    "github.com/yhy0/Jie/pkg/input"
    "github.com/yhy0/Jie/pkg/mitmproxy/go-mitmproxy/proxy"
    "github.com/yhy0/Jie/pkg/protocols/httpx"
    "github.com/yhy0/Jie/pkg/task"
    "github.com/yhy0/Jie/pkg/util"
    "github.com/yhy0/Jie/scan/gadget/waf"
    "github.com/yhy0/logging"
    "github.com/yhy0/sizedwaitgroup"
    "net/url"
    "path"
    "regexp"
    "strings"
    "time"
)

/**
  @author: yhy
  @since: 2023/1/11
  @desc: 对主动爬虫扫描结果的数据处理
**/

// 默认过滤的后缀名
var extensionFilter = []string{
    ".ico", ".ttf",
}

// Active 主动扫描 调用爬虫扫描, 只会输入一个域名
func Active(target string, fingerprint []string) ([]string, []string) {
    if target == "" {
        logging.Logger.Errorln("target must be set")
        return nil, nil
    }
    
    // 超时控制，超过 10 分钟就丢掉吧
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()
    doneCh := make(chan bool, 1)
    defer close(doneCh)
    
    for {
        doneCh <- true
        var subdomains []string
        
        // 判断是否以 http https 开头
        httpMatch, _ := regexp.MatchString("^(http)s?://", target)
        if !httpMatch {
            portMatch, _ := regexp.MatchString(":443", target)
            if portMatch {
                target = fmt.Sprintf("https://%s", target)
            } else {
                target = fmt.Sprintf("http://%s", target)
            }
        }
        
        parseUrl, err := url.Parse(target)
        if err != nil {
            logging.Logger.Errorln(err)
            return nil, nil
        }
        var host string
        // 有的会带80、443端口号，导致    example.com 和 example.com:80、example.com:443被认为是不同的网站
        if strings.Contains(parseUrl.Host, ":443") || strings.Contains(parseUrl.Host, ":80") {
            host = strings.Split(parseUrl.Host, ":")[0]
        } else {
            host = parseUrl.Host
        }
        
        t := &task.Task{
            Parallelism: conf.Parallelism,
            ScanTask:    make(map[string]*task.ScanTask),
        }
        
        client := httpx.NewClient(nil)
        t.ScanTask[host] = &task.ScanTask{
            PerServer: make(map[string]bool),
            PerFolder: make(map[string]bool),
            PocPlugin: make(map[string]bool),
            Client:    client,
            // 3: 同时运行 3 个插件，2 供 PreServer、 PreFolder这两个函数使用，防止马上退出 所以这里同时运行的插件个数为3-5 个
            // TODO 更优雅的实现方式
            Wg: sizedwaitgroup.New(3 + 3),
        }
        
        pool, _ := ants.NewPool(t.Parallelism)
        t.Pool = pool
        defer t.Pool.Release() // 释放协程池
        // 爬虫前，进行连接性、指纹识别、 waf 探测
        resp, err := client.Request(target, "GET", "", nil)
        if err != nil {
            logging.Logger.Errorln("End: ", err)
            return nil, nil
        }
        
        technologies := fingprints.Identify([]byte(resp.Body), resp.Header)
        
        wafs := waf.Scan(target, resp.Body, client)
        
        // 爬虫的同时进行指纹识别
        if conf.GlobalConfig.WebScan.Craw == "c" {
            logging.Logger.Infoln("Crawling with Crawlergo.")
            subdomains = Crawlergo(ctx, target, wafs, t, fingerprint)
        } else {
            logging.Logger.Infoln("Crawling with Katana.")
            subdomains = Katana(ctx, target, wafs, t, fingerprint)
        }
        
        t.WG.Wait()
        
        logging.Logger.Debugln("Fingerprints: ", t.Fingerprints)
        
        t.Fingerprints = funk.UniqString(append(t.Fingerprints, technologies...))
        
        select {
        case <-ctx.Done():
            // 如果超时，将会接收到这个信号
            logging.Logger.Warningln("Jie 运行超时，退出扫描", target)
            return subdomains, t.Fingerprints
        case <-doneCh:
            return subdomains, t.Fingerprints
        }
    }
}

func Katana(ctx context.Context, target string, waf []string, t *task.Task, fingerprint []string) []string {
    parseUrl, err := url.Parse(target)
    if err != nil {
        logging.Logger.Errorln(err)
        return nil
    }
    rootHostname := parseUrl.Host
    
    i := 0
    now := time.Now()
    out := func(result output.Result) { // Callback function to execute for result
        if checkCancellation(ctx) {
            return
        }
        curl := strings.ReplaceAll(result.Request.URL, "\\n", "")
        curl = strings.ReplaceAll(curl, "\\t", "")
        curl = strings.ReplaceAll(curl, "\\n", "")
        parseUrl, err = url.Parse(curl)
        if err != nil {
            logging.Logger.Errorln(err)
            return
        }
        extension := strings.ToLower(path.Ext(parseUrl.Path))
        
        if util.InSlice(crawler.ExtensionFilter, extension) {
            return
        }
        
        logging.Logger.Infof("Katana: [%s] %v %v", result.Request.Method, result.Request.URL, result.Request.Body)
        i++
        
        // 只要这个域名的，其他的都不要
        if !strings.EqualFold(parseUrl.Host, rootHostname) {
            return
        }
        
        var host string
        // 有的会带80、443端口号，导致    example.com 和 example.com:80、example.com:443被认为是不同的网站
        if strings.Contains(parseUrl.Host, ":443") || strings.Contains(parseUrl.Host, ":80") {
            host = strings.Split(parseUrl.Host, ":")[0]
        } else {
            host = parseUrl.Host
        }
        var resp *httpx.Response
        resp, err = t.ScanTask[host].Client.Request(result.Request.URL, result.Request.Method, result.Request.Body, result.Request.Headers)
        if err != nil {
            logging.Logger.Errorln(err)
            return
        }
        
        headers := make(map[string][]string)
        
        for k, v := range result.Request.Headers {
            headers[k] = strings.Split(v, ", ")
        }
        
        _fingerprint := fingprints.Identify([]byte(result.Request.Body), headers)
        
        fingerprint = funk.UniqString(append(_fingerprint, fingerprint...))
        
        // 对爬虫结果格式化
        var crawlResult = &input.CrawlResult{
            Url:          result.Request.URL,
            ParseUrl:     parseUrl,
            Host:         host,
            Target:       target,
            Method:       result.Request.Method,
            Source:       result.Request.Source,
            Headers:      result.Request.Headers,
            RequestBody:  result.Request.Body,
            Fingerprints: fingerprint,
            Waf:          waf,
            Resp:         resp,
            // UniqueId:     util.UUID(), // 这里爬虫中已经判断过了，所以生成一个 uuid 就行
            // 需要先自己实现，Katana 去重逻辑不太行
            UniqueId: util.UniqueId(&proxy.Request{
                Method: result.Request.Method,
                URL:    parseUrl,
                Header: headers,
                Body:   []byte(result.Request.Body),
            }),
            RawRequest:  result.Request.Raw,
            RawResponse: result.Response.Raw,
        }
        
        // 分发扫描任务
        t.WG.Add(1)
        err = t.Pool.Submit(t.Distribution(crawlResult))
        if err != nil {
            t.WG.Done()
            logging.Logger.Errorf("Katana add distribution err:%v, crawlResult:%v", err, crawlResult)
        }
    }
    
    if conf.GlobalConfig.WebScan.Craw == "k" {
        crawler.Katana(target, false, false, out)
    } else {
        crawler.Katana(target, true, conf.GlobalConfig.WebScan.Show, out)
    }
    
    logging.Logger.Infof("Task finished, %d results, %d subdomains found, runtime: %d s", i, 0, time.Now().Unix()-now.Unix())
    
    return nil
}

// Crawlergo 运行爬虫, 对爬虫结果进行处理
func Crawlergo(ctx context.Context, target string, waf []string, t *task.Task, fingerprint []string) []string {
    var targets []*model.Request
    
    var req model.Request
    u, err := model.GetUrl(target)
    if err != nil {
        logging.Logger.Error("parse url failed, ", err)
        return nil
    }
    
    req = model.GetRequest(config.GET, u, getOption())
    req.Proxy = crawler.TaskConfig.Proxy
    targets = append(targets, &req)
    
    if len(targets) != 0 {
        logging.Logger.Infof("Init crawler task, host: %s, max tab count: %d, max crawl count: %d, max runtime: %d %s", targets[0].URL.Host, crawler.TaskConfig.MaxTabsCount, crawler.TaskConfig.MaxCrawlCount, crawler.TaskConfig.MaxRunTime, "s")
    } else {
        logging.Logger.Errorln("no validate target.")
        return nil
    }
    
    if crawler.TaskConfig.Proxy != "" {
        logging.Logger.Info("request with proxy: ", crawler.TaskConfig.Proxy)
    }
    
    // 实时获取结果
    onResult := func(result *crawlergo.OutResult) {
        if checkCancellation(ctx) {
            return
        }
        // 不对这些进行漏扫
        for _, suffix := range extensionFilter {
            if strings.HasSuffix(result.ReqList.URL.Path, suffix) {
                return
            }
        }
        
        logging.Logger.Infof("crawlergo: [%s] %v %v", result.ReqList.Method, result.ReqList.URL.String(), result.ReqList.PostData)
        
        curl := strings.ReplaceAll(result.ReqList.URL.String(), "\\n", "")
        curl = strings.ReplaceAll(curl, "\\t", "")
        curl = strings.ReplaceAll(curl, "\\n", "")
        parseUrl, err := url.Parse(curl)
        if err != nil {
            logging.Logger.Errorln(err)
            return
        }
        
        var host string
        // 有的会带80、443端口号，导致    example.com 和 example.com:80、example.com:443被认为是不同的网站
        if strings.Contains(parseUrl.Host, ":443") || strings.Contains(parseUrl.Host, ":80") {
            host = strings.Split(parseUrl.Host, ":")[0]
        } else {
            host = parseUrl.Host
        }
        resp, err := t.ScanTask[host].Client.Request(result.ReqList.URL.String(), result.ReqList.Method, result.ReqList.PostData, nil)
        if err != nil {
            logging.Logger.Errorln(err)
            return
        }
        
        _fingerprint := fingprints.Identify([]byte(resp.Body), resp.Header)
        
        fingerprint = funk.UniqString(append(_fingerprint, fingerprint...))
        
        // 对爬虫结果格式化
        var crawlResult = &input.CrawlResult{
            Url:          curl,
            ParseUrl:     parseUrl,
            Host:         host,
            Target:       target,
            Method:       result.ReqList.Method,
            Source:       result.ReqList.Source,
            Headers:      make(map[string]string),
            RequestBody:  result.ReqList.PostData,
            Fingerprints: fingerprint,
            Waf:          waf,
            Resp:         resp,
            UniqueId:     util.UUID(), // 这里爬虫中已经判断过了，所以生成一个 uuid 就行
        }
        
        // 分发扫描任务
        t.WG.Add(1)
        err = t.Pool.Submit(t.Distribution(crawlResult))
        if err != nil {
            t.WG.Done()
            logging.Logger.Errorf("Crawlergo add distribution err:%v, crawlResult:%v", err, crawlResult)
        }
    }
    
    // 开始爬虫任务
    crawlerTask, err := crawlergo.NewCrawlerTask(targets, crawler.TaskConfig, onResult)
    if err != nil {
        logging.Logger.Error("create crawler task failed.")
        return nil
    }
    
    crawlerTask.Browser = crawler.Browser
    
    crawlerTask.Run()
    
    logging.Logger.Infof("Task finished, %d results, %d subdomains found, runtime: %d s", len(crawlerTask.Result.ReqList), len(crawlerTask.Result.SubDomainList), time.Now().Unix()-crawlerTask.Start.Unix())
    
    return crawlerTask.Result.SubDomainList
}

func getOption() model.Options {
    var option model.Options
    
    if crawler.TaskConfig.ExtraHeadersString != "" {
        err := json.Unmarshal([]byte(crawler.TaskConfig.ExtraHeadersString), &crawler.TaskConfig.ExtraHeaders)
        if err != nil {
            logging.Logger.Errorln("custom headers can't be Unmarshal.", err)
        }
        option.Headers = crawler.TaskConfig.ExtraHeaders
    }
    return option
}

// 检查上下文是否被取消
func checkCancellation(ctx context.Context) bool {
    select {
    case <-ctx.Done():
        return true
    default:
        return false
    }
}
