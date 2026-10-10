CXX      ?= g++
CXXFLAGS ?= -O2 -Wall -Wextra -std=c++17
LDLIBS   ?= -lftxui-component -lftxui-dom -lftxui-screen
TARGET    = humanix-installer

all: $(TARGET)

$(TARGET): main.cpp
	$(CXX) $(CXXFLAGS) -o $(TARGET) main.cpp $(LDLIBS)

install: $(TARGET)
	install -D -m 0755 $(TARGET) /usr/local/sbin/$(TARGET)

clean:
	rm -f $(TARGET)

.PHONY: all install clean