package sensitive

import (
    regexp "github.com/wasilibs/go-re2"
    "github.com/yhy0/Jie/pkg/output"
    "github.com/yhy0/logging"
    "strings"
    "sync"
    "time"
)

/**
   @author yhy
   @since 2023/10/18
   @desc 检查页面报错信息
    https://github.com/w-digital-scanner/w13scan/blob/master/W13SCAN/lib/helper/helper_sensitive.py
**/

type ErrorMessage struct {
    Text string `json:"text"`
    Type string `json:"type"`
}

var errors = []ErrorMessage{
    {`"Message":"Invalid web service call`, "ASP.Net"},
    {`Exception of type`, "ASP.Net"},
    {`--- End of inner exception stack trace ---`, "ASP.Net"},
    {`Microsoft OLE DB Provider`, "ASP.Net"},
    {`Error ([\d-]+) \([\dA-Fa-f]+\)`, "ASP.Net"},
    {`at ([a-zA-Z0-9_]*\.)*([a-zA-Z0-9_]+)\([a-zA-Z0-9, \[\]\&\;]*\)`, "ASP.Net"},
    {`([A-Za-z]+[.])+[A-Za-z]*Exception: `, "ASP.Net"},
    {`in [A-Za-z]:\([A-Za-z0-9_]+\)+[A-Za-z0-9_\-]+(\.aspx)?\.cs:line [\d]+`, "ASP.Net"},
    {`Syntax error in string in query expression`, "ASP.Net"},
    {`\.java:[0-9]+`, "Java"},
    {`\.java\((Inlined )?Compiled Code\)`, "Java"},
    {`\.invoke\(Unknown Source\)`, "Java"},
    {`nested exception is`, "Java"},
    {`\.js:[0-9]+:[0-9]+`, "Javascript"},
    {`JBWEB[0-9]{{6}}:`, "JBoss"},
    // 误报多
    // {`((dn|dc|cn|ou|uid|o|c)=[\w\d]*,\s?){2,}`, "LDAP"},
    {`\[(ODBC SQL Server Driver|SQL Server|ODBC Driver Manager)\]`, "Microsoft SQL Server"},
    {`Cannot initialize the data source object of OLE DB provider "[\w]*" for linked server "[\w]*"`, "Microsoft SQL Server"},
    {`You have an error in your SQL syntax; check the manual that corresponds to your MySQL server version for the right syntax to use near`, "MySQL"},
    {`Illegal mix of collations \([\w\s\,]+\) and \([\w\s\,]+\) for operation`, "MySQL"},
    {`at (/[A-Za-z0-9\.]+)*\.pm line [0-9]+`, "Perl"},
    {`\.php on line [0-9]+`, "PHP"},
    {`\.php</b> on line <b>[0-9]+`, "PHP"},
    {`Fatal error:`, "PHP"},
    {`\.php:[0-9]+`, "PHP"},
    {`Traceback \(most recent call last\):`, "Python"},
    {`File "[A-Za-z0-9\-_\./]*", line [0-9]+, in`, "Python"},
    {`\.rb:[0-9]+:in`, "Ruby"},
    {`\.scala:[0-9]+`, "Scala"},
    {`\(generated by waitress\)`, "Waitress Python server"},
    {`132120c8|38ad52fa|38cf013d|38cf0259|38cf025a|38cf025b|38cf025c|38cf025d|38cf025e|38cf025f|38cf0421|38cf0424|38cf0425|38cf0427|38cf0428|38cf0432|38cf0434|38cf0437|38cf0439|38cf0442|38cf07aa|38cf08cc|38cf04d7|38cf04c6|websealerror`, "WebSEAL"},
    {`<title>Invalid\sfile\sname\sfor\smonitoring:\s'([^']*)'\.\sFile\snames\sfor\smonitoring\smust\shave\sabsolute\spaths\,\sand\sno\swildcards\.<\/title>`, "ASPNETPathDisclosure"},
    {`You are seeing this page because development mode is enabled.  Development mode, or devMode, enables extra`, "Struts2DevMod"},
    {`You're seeing this error because you have <code>DEBUG = True<\/code> in`, "Django DEBUG MODEL"},
    {`<title>Action Controller: Exception caught<\/title>`, "RailsDevMode"},
    {`Required\s\w+\sparameter\s'([^']+?)'\sis\snot\spresent`, "RequiredParameter"},
    {`<p class="face">:\(</p>`, "Thinkphp3 Debug"},
    {`class='xdebug-error xe-fatal-error'`, "xdebug"},
}

var seenRequests sync.Map // 这里主要是为了一些返回包检测类的判断是否识别过，减小开销，扫描类内部会判断是否扫描过

type Regexp struct {
    Re  *regexp.Regexp
    Msg ErrorMessage
}

var errorCompiled map[string]*Regexp

func init() {
    // 只编译一次编译正则
    errorCompiled = make(map[string]*Regexp, len(errors))
    for _, errorMsg := range errors {
        errorCompiled[errorMsg.Text] = &Regexp{
            Re:  regexp.MustCompile(errorMsg.Text),
            Msg: errorMsg,
        }
    }
}

func PageErrorMessageCheck(url, req, body string) []ErrorMessage {
    // 因为放到了 httpx.Request 中，所以会有很多重复，这里检验一下 url 是否已经检测过了
    if _, ok := seenRequests.Load(url); ok {
        return nil
    }
    seenRequests.Store(url, true)
    
    var results []ErrorMessage
    for _, errorMsg := range errorCompiled {
        re := errorMsg.Re
        result := re.FindString(body)
        if result != "" {
            // org.springframework.web.HttpRequestMethodNotSupportedException 这种也会匹配到，java 这样的会误报混淆
            if "([A-Za-z]+[.])+[A-Za-z]*Exception: " == errorMsg.Msg.Text && strings.Contains(body, ".java") {
                continue
            }
            
            results = append(results, ErrorMessage{
                Text: result,
                Type: errorMsg.Msg.Type,
            })
            
            output.OutChannel <- output.VulMessage{
                DataType: "web_vul",
                Plugin:   "Sensitive error",
                VulnData: output.VulnData{
                    CreateTime: time.Now().Format("2006-01-02 15:04:05"),
                    VulnType:   errorMsg.Msg.Text,
                    Target:     url,
                    Payload:    result,
                    Request:    req,
                    Response:   body,
                },
                Level: output.Low,
            }
            logging.Logger.Infoln("[Sensitive]", url, errorMsg.Msg.Type, result)
        }
    }
    
    return results
}
