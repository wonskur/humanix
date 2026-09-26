CXX ?= g++
CXXFLAGS ?= -O2 -Wall -Wextra -std=c++17

all: sys-sentinel sentinel-ctl

sys-sentinel: main.cpp
	$(CXX) $(CXXFLAGS) -o sys-sentinel main.cpp

sentinel-ctl: sentinel-ctl.cpp
	$(CXX) $(CXXFLAGS) -o sentinel-ctl sentinel-ctl.cpp

install: sys-sentinel sentinel-ctl
	install -D -m 0755 sys-sentinel /usr/local/bin/sys-sentinel
	install -D -m 0755 sentinel-ctl /usr/local/bin/sentinel-ctl

clean:
	rm -f sys-sentinel sentinel-ctl

.PHONY: all install clean
