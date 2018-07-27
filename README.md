# shadaproxy-agent
一个用于穿透NAT，访问内网服务的工具

## 编译、运行
此服务部署在能访问到目标服务的机器上，Windows上需要在cygwin下编译。

    $ go get github.com/liuaifu/buffer
	//仅windows上需要winservice
    $ go get github.com/liuaifu/winservice
	$ git clone https://github.com/liuaifu/shadaproxy-agent.git
	$ cd shadaproxy-agent
	$ go build
	$ cp config.xml.sample config.xml
	$ vim config.xml
	$ ./shadaproxy-agent

Windows上可以注册为系统服务：<br/>

	//注意等号后的空格不能少
	sc create shadaproxy-agent start= auto binPath= X:\xxxx\shadaproxy-agent.exe
	sc start shadaproxy-agent

## 配置示例
	<config>
		<!--服务器地址-->
		<server_addr>x.x.x.x:xxxx</server_addr>
		<!--目标服务列表，可以配多个服务-->
		<services>
			<service>
				<!--服务名称-->
				<name>rdp</name>
				<!--与服务器端配置的key相同-->
				<key>1234abcd</key>
				<!--目标服务地址-->
				<service_addr>127.0.0.1:3389</service_addr>
				<!--始终保证与服务器间空闲连接的数量-->
				<pool_size>3</pool_size>
			</service>
			<!--
			<service>
				<key>5678</key>
				<service_addr>127.0.0.1:21</service_addr>
				<pool_size>3</pool_size>
			</service>
			-->
		</services>
	</config>


## 服务端
服务端需部署在公网上，代码地址：<br/>
[https://github.com/liuaifu/shadaproxy-server](https://github.com/liuaifu/shadaproxy-server)
