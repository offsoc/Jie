package crawlergo

import (
    "github.com/yhy0/Jie/crawler/crawlergo/model"
    "strings"
    
    mapset "github.com/deckarep/golang-set/v2"
)

func SubDomainCollect(reqList []*model.Request, HostLimit string) []string {
    var subDomainList []string
    uniqueSet := mapset.NewSet[string]()
    for _, req := range reqList {
        domain := req.URL.Hostname()
        if uniqueSet.Contains(domain) {
            continue
        }
        uniqueSet.Add(domain)
        if strings.HasSuffix(domain, "."+HostLimit) {
            subDomainList = append(subDomainList, domain)
        }
    }
    return subDomainList
}

func AllDomainCollect(reqList []*model.Request) []string {
    uniqueSet := mapset.NewSet[string]()
    var allDomainList []string
    for _, req := range reqList {
        domain := req.URL.Hostname()
        if uniqueSet.Contains(domain) {
            continue
        }
        uniqueSet.Add(domain)
        allDomainList = append(allDomainList, req.URL.Hostname())
    }
    return allDomainList
}
