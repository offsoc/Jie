package ast

import (
    "github.com/tdewolff/parse/v2"
    "github.com/tdewolff/parse/v2/js"
    "github.com/thoas/go-funk"
    "github.com/yhy0/logging"
    "io"
    "runtime"
    "strings"
)

/**
  @author: yhy
  @since: 2023/5/5
  @desc: //TODO
**/

func SearchInputInScript(input, script string) (Occurrences []Occurence) {
    p := Parser{}
    Index := 0
    if len(script) == 0 {
        logging.Logger.Warning("SearchInputInScript 获取失败")
        return
    }
    p.jsParser(script)
    tokens := p.GetRoot()
    defer p.Clear()
    
    if tokens.Length() == 0 {
        return
    }
    
    for _, token := range tokens.Children {
        tagName := token.Value.TagName
        content := token.Value.Content
        
        if !funk.Contains(content, input) {
            continue
        }
        
        Occurrences = append(Occurrences, Occurence{Type: tagName, Position: Index, Details: CopyNode(token)})
    }
    
    return
}

func (parser *Parser) jsParser(script string) {
    // 捕获 parser.tokenizer.Insert 可能发生的异常
    defer func() {
        if err := recover(); err != nil {
            logging.Logger.Errorln("parser.tokenizer.Insert recover from:", err)
            debugStack := make([]byte, 1024)
            runtime.Stack(debugStack, false)
            logging.Logger.Errorf("Stack Trace:%v", string(debugStack))
            
        }
    }()
    
    if parser.tokenizer == nil {
        parser.tokenizer = new(Node)
    }
    
    // Parse the JavaScript code into an AST.xw
    l := js.NewLexer(parse.NewInputString(script))
    
    var i = 0
    for {
        i++
        tt, text := l.Next()
        commentText := string(text)
        Attributes := make([]*Attribute, 0)
        switch tt {
        case js.ErrorToken:
            if l.Err() != io.EOF {
                logging.Logger.Debugln("Error on line:", l.Err())
            }
            return
        case js.CommentToken: // 注释
            // 单行注释
            if !strings.Contains(commentText, "\n") {
                parser.tokenizer.Insert(i, "InlineComment", &commentText, &Attributes)
            } else {
                // 多行注释
                parser.tokenizer.Insert(i, "BlockComment", &commentText, &Attributes)
            }
        case js.IdentifierToken: // 变量名
            parser.tokenizer.Insert(i, "ScriptIdentifier", &commentText, &Attributes)
        case js.StringToken: // 变量值
            parser.tokenizer.Insert(i, "ScriptLiteral", &commentText, &Attributes)
        }
        
    }
}
