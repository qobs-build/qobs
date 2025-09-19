#include <windows.h>
const TCHAR g_szClassName[] = TEXT("MyTriangleWindow");

LRESULT CALLBACK WndProc(HWND hwnd, UINT msg, WPARAM wParam, LPARAM lParam);

int WINAPI WinMain(HINSTANCE hInstance, HINSTANCE hPrevInstance,
                   LPSTR lpCmdLine, int nCmdShow)
{
    WNDCLASSEX wc;
    HWND hwnd;
    MSG Msg;

    // register window class
    wc.cbSize        = sizeof(WNDCLASSEX);
    wc.style         = CS_HREDRAW | CS_VREDRAW;
    wc.lpfnWndProc   = WndProc;
    wc.cbClsExtra    = 0;
    wc.cbWndExtra    = 0;
    wc.hInstance     = hInstance;
    wc.hIcon         = LoadIcon(NULL, IDI_APPLICATION);
    wc.hCursor       = LoadCursor(NULL, IDC_ARROW);
    wc.hbrBackground = (HBRUSH)(COLOR_WINDOW + 1);
    wc.lpszMenuName  = NULL;
    wc.lpszClassName = g_szClassName;
    wc.hIconSm       = LoadIcon(NULL, IDI_APPLICATION);

    if (!RegisterClassEx(&wc))
    {
        MessageBox(NULL, TEXT("Window Registration Failed!"), TEXT("Error!"),
                    MB_ICONEXCLAMATION | MB_OK);
        return 0;
    }

    // create the window
    hwnd = CreateWindowEx(
        0,
        g_szClassName,
        TEXT("GDI Triangle Example"),
        WS_OVERLAPPEDWINDOW,
        CW_USEDEFAULT, CW_USEDEFAULT, 400, 300,
        NULL, NULL, hInstance, NULL);

    if (hwnd == NULL)
    {
        MessageBox(NULL, TEXT("Window Creation Failed!"), TEXT("Error!"),
                    MB_ICONEXCLAMATION | MB_OK);
        return 0;
    }

    ShowWindow(hwnd, nCmdShow);
    UpdateWindow(hwnd);

    // message loop
    while (GetMessage(&Msg, NULL, 0, 0) > 0)
    {
        TranslateMessage(&Msg);
        DispatchMessage(&Msg);
    }
    return (int)Msg.wParam;
}

LRESULT CALLBACK WndProc(HWND hwnd, UINT msg, WPARAM wParam, LPARAM lParam)
{
    switch (msg)
    {
    case WM_PAINT:
        {
            PAINTSTRUCT ps;
            HDC hdc = BeginPaint(hwnd, &ps);

            // pen for the triangle outline
            HPEN hPen = CreatePen(PS_SOLID, 3, RGB(255, 0, 0));
            HPEN hOldPen = (HPEN)SelectObject(hdc, hPen);

            // brush for filling the triangle
            HBRUSH hBrush = CreateSolidBrush(RGB(0, 255, 0));
            HBRUSH hOldBrush = (HBRUSH)SelectObject(hdc, hBrush);

            // triangle points
            POINT pts[3];
            pts[0].x = 200; pts[0].y = 50;
            pts[1].x = 50;  pts[1].y = 200;
            pts[2].x = 350; pts[2].y = 200;

            // draw with fill
            Polygon(hdc, pts, 3);

            // cleanup
            SelectObject(hdc, hOldBrush);
            DeleteObject(hBrush);

            SelectObject(hdc, hOldPen);
            DeleteObject(hPen);

            EndPaint(hwnd, &ps);
        }
        break;

    case WM_DESTROY:
        PostQuitMessage(0);
        break;

    default:
        return DefWindowProc(hwnd, msg, wParam, lParam);
    }
    return 0;
}
