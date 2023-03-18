package cmd

import (
	"fmt"
	"github.com/logrusorgru/aurora"
	"github.com/thoas/go-funk"
	"github.com/urfave/cli/v2"
	"github.com/yhy0/Jie/conf"
	"github.com/yhy0/Jie/pkg/output"
	"github.com/yhy0/Jie/pkg/protocols/headless"
	"github.com/yhy0/Jie/pkg/protocols/httpx"
	"github.com/yhy0/Jie/pkg/task"
	"github.com/yhy0/Jie/pkg/util"
	"github.com/yhy0/logging"
	"os"
	"os/signal"
	"syscall"
)

/**
  @author: yhy
  @since: 2023/1/3
  @desc: //TODO
**/

var (
	Plugins cli.StringSlice
	Poc     cli.StringSlice
	Proxy   string
	Listen  string
	target  string
	debug   bool
	show    bool
)

func init() {
	fmt.Println("\t" + aurora.Green(conf.Banner).String())
	fmt.Println("\t\t" + aurora.Red("v"+conf.Version).String())
	fmt.Println("\t" + aurora.Blue(conf.Website).String() + "\n\n")

	fmt.Println(aurora.Red("Use with caution. You are responsible for your actions.").String())
	fmt.Println(aurora.Red("Developers assume no liability and are not responsible for any misuse or damage.").String() + "\n")
}

func RunApp() {
	app := &cli.App{
		Name:  "Jie",
		Usage: "A powerful web security assessment tool",
		Commands: []*cli.Command{
			{
				Name:    "webscan",
				Aliases: []string{"ws"},
				Usage:   "Run a webscan task",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "browser-crawler",
						Aliases:     []string{"browser"},
						Usage:       "use a browser spider to crawl the target and scan the requests",
						Destination: &target,
						Required:    true,
					},
					// 设置需要开启的插件
					&cli.StringSliceFlag{
						Name:        "plugin",
						Usage:       "Vulnerable Plugin, (example: --plugin xss,csrf,sql,bbscan ...)",
						Destination: &Plugins,
					},
					// 设置需要开启的插件
					&cli.BoolFlag{
						Name:        "show",
						Usage:       "specifies whether the show the browser in headless mode",
						Destination: &show,
					},

					// 设置需要开启的插件
					&cli.StringSliceFlag{
						Name:        "poc",
						Usage:       "specify the poc to run, separated by ','(example: test.yml,./test/*)",
						Destination: &Poc,
					},
					// 设置代理
					&cli.StringFlag{
						Name:        "proxy",
						Usage:       "Proxy, (example: --proxy http://127.0.0.1:8080)",
						Destination: &Proxy,
					},
					// 被动监听，收集流量
					&cli.StringFlag{
						Name:        "listen",
						Usage:       "use proxy resource collector, value is proxy addr, (example: 127.0.0.1:1111)",
						Destination: &Listen,
					},
					//
					&cli.BoolFlag{
						Name:        "debug",
						Usage:       "debug",
						Destination: &debug,
					},
				},
				Action: run,
			},
			{
				Name:    "generate-ca-cert",
				Aliases: []string{"genca"},
				Usage:   "GenerateToFile CA certificate and key",
				Action: func(cCtx *cli.Context) error {
					fmt.Println("genca : ", cCtx.Args().First())
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		logging.Logger.Fatal(err)
	}
}

func run(c *cli.Context) error {
	logging.New(debug, "Jie")

	go func() {
		for v := range output.OutChannel {
			logging.Logger.Infoln(aurora.Red(v.PrintScreen()).String())
		}
	}()

	var plugins []string
	if len(Plugins.Value()) == 0 {
		plugins = conf.DefaultPlugins
	} else {
		plugins = util.ToUpper(Plugins.Value())
	}

	conf.GlobalConfig = &conf.Config{}

	conf.GlobalConfig.WebScan.Proxy = Proxy
	conf.GlobalConfig.WebScan.Plugins = plugins
	conf.GlobalConfig.WebScan.Poc = Poc.Value()
	conf.GlobalConfig.Reverse.Domain = ""
	conf.GlobalConfig.Reverse.Token = ""

	// 初始化 session ,todo 后续优化一下，不同网站共用一个不知道会不会出问题，应该不会
	httpx.NewSession()

	// 初始化 rod
	if funk.Contains(conf.GlobalConfig.WebScan.Plugins, "XSS") {
		headless.Rod()
	}

	if Listen != "" {
		// 被动扫描
		task.Passive()
	} else {
		task.Active(target, show)
	}

	cc := make(chan os.Signal)
	// 监听信号
	signal.Notify(cc, syscall.SIGINT)
	go func() {
		for s := range cc {
			switch s {
			case syscall.SIGINT:
				if headless.RodHeadless != nil {
					headless.RodHeadless.Close()
				}
			default:
			}
		}
	}()

	if headless.RodHeadless != nil {
		headless.RodHeadless.Close()
	}

	return nil
}
