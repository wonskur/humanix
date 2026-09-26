CXX ?= g++
CXXFLAGS ?= -O2 -Wall -Wextra -std=c++17
TARGET = dish

all: $(TARGET)

$(TARGET): main.cpp
	$(CXX) $(CXXFLAGS) -o $(TARGET) main.cpp

install: $(TARGET)
	install -D -m 0755 $(TARGET) /usr/local/bin/$(TARGET)

clean:
	rm -f $(TARGET)

.PHONY: all install clean
