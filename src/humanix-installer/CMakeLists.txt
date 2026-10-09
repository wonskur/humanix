cmake_minimum_required(VERSION 3.16)
project(humanix-installer CXX)

set(CMAKE_CXX_STANDARD 17)

find_package(ftxui QUIET)
if(NOT ftxui_FOUND)
  include(FetchContent)
  FetchContent_Declare(
    ftxui
    GIT_REPOSITORY https://github.com/ArthurSonzogni/FTXUI
    GIT_TAG v5.0.0
  )
  FetchContent_MakeAvailable(ftxui)
endif()

add_executable(humanix-installer main.cpp)
target_link_libraries(humanix-installer PRIVATE
  ftxui::screen
  ftxui::dom
  ftxui::component
)